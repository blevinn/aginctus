package testenv

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const unboundVMGrace = 2 * time.Minute

// VMReservation is persisted BEFORE preparing the cgroup and before exec.
// It allows a separately scheduled watchdog to reclaim a crashed startup
// even if the process failed before the run.json PID identity was committed.
type VMReservation struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"run_id"`
	OwnerUID      int       `json:"owner_uid"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

func (s *RunStore) reserveVMRunWithTTL(ttl time.Duration) (string, string, error) {
	if ttl <= 0 || ttl > maxRunLifetime {
		return "", "", errors.New("invalid VM reservation lifetime")
	}
	id, dir, err := s.reserveVMRun()
	if err != nil {
		return "", "", err
	}
	now := time.Now().UTC()
	reservation := VMReservation{
		SchemaVersion: 1, ID: id, OwnerUID: os.Getuid(),
		CreatedAt: now, ExpiresAt: now.Add(ttl),
	}
	file, err := os.OpenFile(filepath.Join(dir, "reservation.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", "", errors.New("cannot create VM reservation")
	}
	writeErr := json.NewEncoder(file).Encode(reservation)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return "", "", errors.New("VM reservation durability not verified")
	}
	// Make run-directory creation durable before creating the cgroup. If the
	// filesystem does not allow synchronization, refuse launch.
	dirHandle, err := os.Open(dir)
	if err != nil {
		return "", "", errors.New("VM reservation directory unavailable")
	}
	syncErr := dirHandle.Sync()
	closeErr = dirHandle.Close()
	if syncErr != nil || closeErr != nil {
		return "", "", errors.New("VM reservation directory sync failed")
	}
	// Sync the *parent* to persist the newly created run directory itself.
	// Otherwise a crash can lose the entry while leaving an owned cgroup.
	rootHandle, err := os.Open(s.root)
	if err != nil {
		return "", "", errors.New("VM run store unavailable for durability")
	}
	syncErr = rootHandle.Sync()
	closeErr = rootHandle.Close()
	if syncErr != nil || closeErr != nil {
		return "", "", errors.New("VM run store sync failed")
	}
	return id, dir, nil
}

func (s *RunStore) readVMReservation(id string) (VMReservation, error) {
	var rec VMReservation
	if !validRunID(id) {
		return rec, errors.New("invalid reserved run")
	}
	dir := filepath.Join(s.root, id)
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || ownedByWorker(info) != nil {
		return rec, errors.New("untrusted VM reservation directory")
	}
	path := filepath.Join(dir, "reservation.json")
	info, err = os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || ownedByWorker(info) != nil {
		return rec, errors.New("VM reservation missing or unsafe")
	}
	if info.Size() > 2048 {
		return rec, errors.New("VM reservation exceeds size limit")
	}
	f, err := os.Open(path)
	if err != nil {
		return rec, errors.New("VM reservation unavailable")
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rec); err != nil {
		return rec, errors.New("invalid VM reservation")
	}
	if rec.SchemaVersion != 1 || rec.ID != id || rec.OwnerUID != os.Getuid() ||
		rec.CreatedAt.IsZero() || !rec.ExpiresAt.After(rec.CreatedAt) ||
		rec.ExpiresAt.Sub(rec.CreatedAt) > maxRunLifetime {
		return VMReservation{}, errors.New("VM reservation identity or bounds invalid")
	}
	return rec, nil
}
