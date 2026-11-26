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
}

// ReconcileExpiredIncusVMs runs one bounded, caller-triggered sweep via the
// same authenticated backend ownership path as interactive lifecycle calls.
// An independent installed worker timer must schedule this later; a coding
// agent cannot select the daemon, project, run root or expiration timestamp.
func ReconcileExpiredIncusVMs(ctx context.Context, backend *IncusVMBackend, now time.Time) (IncusReconcileResult, error) {
	var summary IncusReconcileResult
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
	var failures []error
	for _, vm := range owned {
		if err := ctx.Err(); err != nil {
			return summary, errors.Join(err, errors.Join(failures...))
		}
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
	}
	return summary, errors.Join(failures...)
}
