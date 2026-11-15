package acceptedstate

import (
	"context"
	"errors"
	"slices"
)

// PinnedWorkloadSnapshot is a nonsecret, exact workload projection of an
// already-accepted immutable revision. It is data, not a write capability,
// credential, controller claim, or permission to access Incus.
//
// In particular, its image fingerprint is immutable intent; no mutable
// alias, host device, network policy or ambient CLI configuration is resolved.
type PinnedWorkloadSnapshot struct {
	Revision     Revision     `json:"revision"`
	DeploymentID string       `json:"deploymentID"`
	IncusProject string       `json:"incusProject"`
	Workload     WorkloadSpec `json:"workload"`
}

// SnapshotSelectedWorkload reads the accepted specification while the same-UID
// CAS pin remains held. The caller must keep that pin alive through any
// downstream operation and separately enforce principal/resource authority.
// A successful snapshot is NOT a mutation-time authorization or a fence
// against an independent/root Incus writer.
func (p *LocalRevisionPin) SnapshotSelectedWorkload(ctx context.Context) (PinnedWorkloadSnapshot, error) {
	if p == nil {
		return PinnedWorkloadSnapshot{}, ErrLocalPinScope
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return PinnedWorkloadSnapshot{}, ErrLocalPinScope
	}
	if err := p.check(ctx); err != nil {
		return PinnedWorkloadSnapshot{}, err
	}
	revision, spec, err := p.store.ReadActive()
	if err != nil {
		return PinnedWorkloadSnapshot{}, err
	}
	if revision == nil || spec == nil || *revision != p.revision ||
		spec.Selection.DeploymentID != p.deployment ||
		spec.Selection.IncusProject != p.project {
		return PinnedWorkloadSnapshot{}, ErrRevisionConflict
	}
	if err := ctx.Err(); err != nil {
		return PinnedWorkloadSnapshot{}, err
	}
	for _, item := range spec.Workloads {
		if item.ID == p.workload {
			// Defensively clone the one slice so the returned object cannot
			// modify future snapshots from this in-memory representation.
			item.Profiles = slices.Clone(item.Profiles)
			return PinnedWorkloadSnapshot{
				Revision: p.revision, DeploymentID: p.deployment,
				IncusProject: p.project, Workload: item,
			}, nil
		}
	}
	// A selected workload without a corresponding accepted typed spec must
	// fail closed, not become an implicit create/delete request.
	return PinnedWorkloadSnapshot{}, errors.New("selected accepted workload specification is missing")
}
