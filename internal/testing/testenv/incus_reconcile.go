package testenv

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const MaxIncusReconcileCandidates = 64

// IncusReconcileResult distinguishes verified instance deletion from
// independent root-volume API absence. Neither proves physical reclamation,
// guest evidence or worker filesystem cleanup.
type IncusReconcileResult struct {
	Scanned               int `json:"scanned"`
	Skipped               int `json:"skipped"`
	Destroyed             int `json:"destroyed"`
	Failed                int `json:"failed"`
	RootVolumesAbsent     int `json:"root_volumes_absent"`
	RootVolumesUnverified int `json:"root_volumes_unverified"`
	PendingCleanup        int `json:"pending_cleanup"`
	RecoveredPending      int `json:"recovered_pending"`
}

// ReconcileExpiredIncusVMs runs one bounded, caller-triggered sweep via the
// same authenticated backend ownership path as interactive lifecycle calls.
// An independent installed worker timer must schedule this later; a coding
// agent cannot select the daemon, project, run root or expiration timestamp.
func ReconcileExpiredIncusVMs(ctx context.Context, backend *IncusVMBackend, now time.Time) (IncusReconcileResult, error) {
	return reconcileExpiredIncusVMs(ctx, backend, now, nil)
}

// ReconcileExpiredIncusVMsDurable retains unresolved signed handles across
// sweeps, including when their instance has already disappeared.
func ReconcileExpiredIncusVMsDurable(ctx context.Context, backend *IncusVMBackend, now time.Time, dir string) (IncusReconcileResult, error) {
	if backend == nil || backend.client == nil || now.IsZero() {
		return IncusReconcileResult{}, errors.New("trusted Incus backend and wall clock are required")
	}
	if err := ctx.Err(); err != nil {
		return IncusReconcileResult{}, err
	}
	if _, ok := backend.client.(incusRootVolumeInventory); !ok {
		return IncusReconcileResult{}, ErrUnsupportedAction
	}
	ledger, lock, err := openRecoveryLedger(dir, backend, now)
	if err != nil {
		return IncusReconcileResult{}, err
	}
	defer lock.Close()
	return reconcileExpiredIncusVMs(ctx, backend, now, ledger)
}

func reconcileExpiredIncusVMs(ctx context.Context, backend *IncusVMBackend, now time.Time, ledger *recoveryLedger) (summary IncusReconcileResult, retErr error) {
	if backend == nil || backend.client == nil || now.IsZero() {
		return summary, errors.New("trusted Incus backend and wall clock are required")
	}
	if err := ctx.Err(); err != nil {
		return summary, err
	}
	owned, err := backend.ListOwned(ctx)
	if err != nil {
		return summary, fmt.Errorf("testenv owned VM discovery failed: %w", err)
	}
	if len(owned) > MaxIncusReconcileCandidates {
		return summary, errors.New("testenv VM reconciliation candidate count exceeds bound")
	}
	if ledger != nil {
		if err := ledger.record(owned, now); err != nil {
			return summary, err
		}
		defer func() {
			summary.PendingCleanup = len(ledger.Pending)
			if err := ledger.save(); err != nil {
				retErr = errors.Join(retErr, err)
			}
		}()
	}
	var failures []error
	seen := make(map[string]bool)
	for _, vm := range owned {
		if err := ctx.Err(); err != nil {
			return summary, errors.Join(err, errors.Join(failures...))
		}
		seen[vm.Ref.Instance] = true
		summary.Scanned++
		if now.Before(vm.ExpiresAt) {
			summary.Skipped++
			continue
		}
		if err := backend.destroy(ctx, vm.Ref, now); err != nil {
			summary.Failed++
			failures = append(failures, fmt.Errorf("expired run %s deletion not verified: %w", vm.Ref.RunID, err))
			continue
		}
		summary.Destroyed++
		// A successful Destroy proves only instance absence. A real worker
		// also has the separate project-scoped volume inventory capability.
		inventory, ok := backend.client.(incusRootVolumeInventory)
		if !ok {
			summary.RootVolumesUnverified++
			continue
		}
		present, err := inventory.RootVolumePresent(ctx, backend.pool, vm.Ref.Instance)
		if err != nil || present || ctx.Err() != nil {
			summary.RootVolumesUnverified++
			failures = append(failures, fmt.Errorf("expired run %s root-volume absence not verified", vm.Ref.RunID))
			continue
		}
		summary.RootVolumesAbsent++
		if ledger != nil {
			ledger.resolved(vm.Ref.Instance)
		}
	}
	if ledger != nil {
		// A missing instance is not rediscoverable through ListOwned. Recheck
		// only the signed prior handle, without issuing Stop/Delete on it.
		inventory := backend.client.(incusRootVolumeInventory)
		for _, ref := range append([]VMRef(nil), ledger.Pending...) {
			if seen[ref.Instance] {
				continue
			}
			if err := ctx.Err(); err != nil {
				return summary, errors.Join(err, errors.Join(failures...))
			}
			if err := backend.confirmAbsent(ctx, ref); err != nil {
				failures = append(failures, errors.New("pending instance absence unverified"))
				continue
			}
			present, err := inventory.RootVolumePresent(ctx, backend.pool, ref.Instance)
			if err != nil || present || ctx.Err() != nil {
				failures = append(failures, errors.New("pending root-volume absence unverified"))
				continue
			}
			ledger.resolved(ref.Instance)
			summary.RecoveredPending++
		}
	}
	return summary, errors.Join(failures...)
}
