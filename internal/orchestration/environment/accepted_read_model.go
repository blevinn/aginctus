package environment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/blevinn/aginctus/pkg/api"
)

// PublishedAcceptedSource is supplied by the independently authenticated
// project-state authority. It receives the enrolled descriptor selected by
// Catalog, NOT a user-provided project JSON field or a candidate-file path.
// The data returned still must be checked against that enrolled identity.
type PublishedAcceptedSource interface {
	ReadAccepted(context.Context, Descriptor) (*AcceptedRevision, *Candidate, error)
}

// AcceptedReadModel composes an authenticated environment catalog with a
// project-scoped published revision reader. It exposes no accept, mutation,
// privileged daemon client, or root/user candidate merge.
type AcceptedReadModel struct {
	Catalog Catalog
	Source  PublishedAcceptedSource
}

func projectedEnvironment(d Descriptor) api.Environment {
	return api.Environment{
		ID: d.ID, Name: d.Name,
		Binding: api.EnvironmentBinding{ServerID: d.Binding.ServerID, Project: d.Binding.Project},
	}
}

func projectedRevision(r *AcceptedRevision) *api.AcceptedRevisionIdentity {
	if r == nil {
		return nil
	}
	return &api.AcceptedRevisionIdentity{
		ID: r.ID, Generation: r.Generation,
		ParentID: r.ParentID, CandidateDigest: r.CandidateDigest,
	}
}

func (r AcceptedReadModel) selected(ctx context.Context, project, id string) (Descriptor, *AcceptedRevision, *Candidate, error) {
	if ctx == nil {
		return Descriptor{}, nil, nil, errors.New("state inspection requires context")
	}
	if err := ctx.Err(); err != nil {
		return Descriptor{}, nil, nil, err
	}
	if r.Source == nil {
		return Descriptor{}, nil, nil, errors.New("authenticated project-state source unavailable")
	}
	// Catalog.Get verifies the actual project connection and enrolled
	// environment record via independently supplied authority dependencies.
	descriptor, err := r.Catalog.Get(ctx, project, id)
	if err != nil {
		return Descriptor{}, nil, nil, errors.New("authenticated environment selection unavailable")
	}
	rev, candidate, err := r.Source.ReadAccepted(ctx, descriptor)
	if err != nil {
		return Descriptor{}, nil, nil, errors.New("authenticated accepted revision read unavailable")
	}
	if (rev == nil) != (candidate == nil) {
		return Descriptor{}, nil, nil, errors.New("inconsistent accepted state presence")
	}
	if rev != nil {
		if err := rev.ValidateAgainst(*candidate); err != nil {
			return Descriptor{}, nil, nil, errors.New("accepted state content identity mismatch")
		}
		if !candidate.Environment.SameEnvironment(descriptor) {
			return Descriptor{}, nil, nil, errors.New("accepted state is outside enrolled environment authority")
		}
	}
	if err := ctx.Err(); err != nil {
		return Descriptor{}, nil, nil, err
	}
	return descriptor, rev, candidate, nil
}

// GetAcceptedState is a typed, read-only status projection. An absent
// revision is returned ONLY after the catalog and source successfully read
// the authenticated selected project and environment.
func (r AcceptedReadModel) GetAcceptedState(ctx context.Context, project, environmentID string) (api.AcceptedStateStatus, error) {
	desc, rev, _, err := r.selected(ctx, project, environmentID)
	if err != nil {
		return api.AcceptedStateStatus{}, err
	}
	return api.AcceptedStateStatus{
		TypeMeta:    api.NewTypeMeta("AcceptedStateStatus"),
		Environment: projectedEnvironment(desc),
		Layer:       "accepted", HasAccepted: rev != nil, Revision: projectedRevision(rev),
	}, nil
}

// DiffConfiguredAgainstAccepted compares one explicitly supplied typed
// nonsecret candidate with the PUBLISHED accepted revision for that same
// enrolled environment. It returns field names and workload IDs, not values,
// secrets or a plan to delete unselected resources. The source MUST be an
// authenticated project state authority, never a local fixture assumed trusted.
func digestCandidateBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (r AcceptedReadModel) DiffConfiguredAgainstAccepted(
	ctx context.Context, project, environmentID string, proposed Candidate,
) (api.ConfiguredAcceptedDiff, error) {
	desc, rev, prior, err := r.selected(ctx, project, environmentID)
	if err != nil {
		return api.ConfiguredAcceptedDiff{}, err
	}
	if _, err := SelectCandidate(proposed, desc, desc.Binding); err != nil {
		return api.ConfiguredAcceptedDiff{}, errors.New("configured candidate targets another enrolled environment")
	}
	canonical, err := proposed.Canonical()
	if err != nil {
		return api.ConfiguredAcceptedDiff{}, errors.New("invalid configured candidate")
	}
	proposedDigest := digestCandidateBytes(canonical)
	out := api.ConfiguredAcceptedDiff{
		TypeMeta:    api.NewTypeMeta("ConfiguredAcceptedDiff"),
		Environment: projectedEnvironment(desc),
		FromLayer:   "accepted", ToLayer: "configured",
		HasAccepted: rev != nil, Revision: projectedRevision(rev),
		ProposedDigest: proposedDigest,
		NewlySelected:  []string{}, Unselected: []string{}, Modified: []api.AcceptedWorkloadChange{},
	}
	if rev == nil {
		// There is no accepted baseline. Selected candidate workloads are
		// suggestions for a future explicit promotion, not resource actions.
		out.Changed = true
		out.GatewayChanged = proposed.Gateway != nil
		out.HerdrChanged = proposed.Herdr != nil
		out.NewlySelected = append(out.NewlySelected, proposed.SelectedWorkloads...)
		return out, nil
	}
	diff, err := CompareCandidates(desc, desc.Binding, *prior, proposed)
	if err != nil {
		return api.ConfiguredAcceptedDiff{}, errors.New("cannot compare accepted and configured environment")
	}
	out.Changed = diff.Changed
	out.DisplayNameChanged = diff.DisplayNameChanged
	out.GatewayChanged = diff.GatewayChanged
	out.HerdrChanged = diff.HerdrChanged
	out.NewlySelected = append(out.NewlySelected, diff.WorkloadChanges.NewlySelected...)
	out.Unselected = append(out.Unselected, diff.WorkloadChanges.Unselected...)
	for _, modified := range diff.WorkloadChanges.Modified {
		out.Modified = append(out.Modified, api.AcceptedWorkloadChange{
			ID:     modified.ID,
			Fields: append([]string{}, modified.Fields...),
		})
	}
	return out, nil
}
