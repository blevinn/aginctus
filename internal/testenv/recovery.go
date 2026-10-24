package testenv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const maxRecoveryCandidates = 64

// RecoverySummary records only aggregate, synthetic, run-owned actions.
// This library provides a single recovery pass. It is NOT a watchdog until
// a trusted external service schedules it independently of VM sessions.
type RecoverySummary struct {
	Scanned   int `json:"scanned"`
	Skipped   int `json:"skipped"`
	Recovered int `json:"recovered"`
	Failed    int `json:"failed"`
}

type RecoveryReceipt struct {
	SchemaVersion int       `json:"schema_version"`
	RunID         string    `json:"run_id"`
	CheckedAt     time.Time `json:"checked_at"`
	Reason        string    `json:"reason"`
	Cgroup        string    `json:"cgroup"`
	Cleanup       string    `json:"cleanup"`
}

// writeRecoveryReceipt never follows a preexisting report symlink.
// Reports are atomic, private, bounded in size and overwrite only the
// existing report for the same trusted run ID (never another run).
func (s *RunStore) writeRecoveryReceipt(rec RecoveryReceipt) error {
	if !validRunID(rec.RunID) {
		return errors.New("invalid recovery run ID")
	}
	runDir := filepath.Join(s.root, rec.RunID)
	info, err := os.Lstat(runDir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return errors.New("recovery run directory missing or unsafe")
	}
	if err := ownedByWorker(info); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(runDir, ".recovery-")
	if err != nil {
		return errors.New("cannot reserve private recovery receipt")
	}
	defer os.Remove(tmp.Name())
	writeErr := json.NewEncoder(tmp).Encode(rec)
	if writeErr == nil {
		writeErr = tmp.Sync()
	}
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		return errors.New("cannot write recovery receipt")
	}
	if err := os.Rename(tmp.Name(), filepath.Join(runDir, "recovery.json")); err != nil {
		return errors.New("cannot commit recovery receipt")
	}
	return nil
}

func (s *RunStore) hasPrivateSessionReceipt(id string) bool {
	path := filepath.Join(s.root, id, "session.json")
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm() == 0600 &&
		ownedByWorker(info) == nil
}

// RecoverAbandoned performs a single deterministic scan; an independent
// installed systemd timer/service will have to call it on a schedule.
// It never uses kill(pid), trusts a client-supplied socket or deletes
// workspace data. Only an owned run cgroup can be killed.
func RecoverAbandoned(ctx context.Context, store *RunStore, cgroups *CgroupManager, now time.Time) (RecoverySummary, error) {
	var summary RecoverySummary
	if store == nil || cgroups == nil || now.IsZero() {
		return summary, errors.New("recovery requires trusted run and cgroup stores and a timestamp")
	}
	entries, err := os.ReadDir(store.root)
	if err != nil {
		return summary, errors.New("cannot inspect private VM run store")
	}
	var ids []string
	for _, entry := range entries {
		// Do not resolve unknown entries, symlinks or foreign paths.
		if entry.IsDir() && validRunID(entry.Name()) {
			ids = append(ids, entry.Name())
		}
	}
	if len(ids) > maxRecoveryCandidates {
		return summary, errors.New("too many VM run candidates for a bounded recovery pass")
	}
	var failures []error
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return summary, errors.Join(err, errors.Join(failures...))
		}
		summary.Scanned++
		rec, err := store.ReadRun(id)
		reason := "process_missing_or_changed"
		if err != nil {
			// An absent run.json may mean a crash after the durable
			// reservation but before process binding. A malformed,
			// symlinked or otherwise existing run.json must NOT cause
			// a fallback to reservation-only recovery.
			if _, statErr := os.Lstat(filepath.Join(store.root, id, "run.json")); !os.IsNotExist(statErr) {
				summary.Failed++
				failures = append(failures, fmt.Errorf("run %s has untrusted ownership record", id))
				continue
			}
			reservation, reservationErr := store.readVMReservation(id)
			if reservationErr != nil {
				summary.Failed++
				failures = append(failures, fmt.Errorf("run %s lacks a trusted startup reservation", id))
				continue
			}
			if now.Before(reservation.CreatedAt.Add(unboundVMGrace)) {
				summary.Skipped++
				continue
			}
			reason = "startup_unbound"
		} else {
			liveStart, lookupErr := processIdentity(rec.PID)
			if now.Before(rec.ExpiresAt) && lookupErr == nil && liveStart == rec.ProcessStart {
				summary.Skipped++
				continue
			}
			if !now.Before(rec.ExpiresAt) {
				reason = "expired"
			}
		}
		// A completed run with independently recorded session evidence
		// and no remaining run cgroup requires no further action.
		path := filepath.Join(cgroups.root, id)
		if _, err := os.Lstat(path); os.IsNotExist(err) && store.hasPrivateSessionReceipt(id) {
			summary.Skipped++
			continue
		}
		receipt := RecoveryReceipt{
			SchemaVersion: 1, RunID: id, CheckedAt: now.UTC(),
			Reason: reason, Cgroup: "not_verified", Cleanup: "not_verified",
		}
		cg, err := openRunCgroup(path)
		if err == nil {
			err = cg.KillAndVerify()
			if err == nil {
				err = cg.Remove()
			} else {
				_ = cg.Close()
			}
		}
		failed := err != nil
		if err != nil {
			failures = append(failures, fmt.Errorf("run %s could not verify cgroup removal: %w", id, err))
		} else {
			receipt.Cgroup = "empty_and_removed"
		}
		if err := store.writeRecoveryReceipt(receipt); err != nil {
			failures = append(failures, fmt.Errorf("run %s recovery receipt unavailable: %w", id, err))
			failed = true
		}
		if failed {
			summary.Failed++
		} else {
			summary.Recovered++
		}
	}
	return summary, errors.Join(failures...)
}
