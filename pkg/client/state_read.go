package client

import (
	"context"
	"errors"

	"github.com/blevinn/aginctus/pkg/api"
)

// ConfiguredAcceptedDiffReader computes a read-only comparison from a
// separately verified configured candidate and authenticated accepted state.
// It MUST NOT treat unselected workloads as an implicit removal request.
type ConfiguredAcceptedDiffReader interface {
	DiffConfiguredAccepted(context.Context, string, string) (api.ConfiguredAcceptedDiff, error)
}

type ReadStateRequest struct {
	Project       string
	EnvironmentID string
}

// Accepted delegates to the already validated accepted-state client contract.
// Authentication absence, authorization denial and backend failure are errors,
// never a successful "hasAccepted:false" result.
func (s *StateService) Accepted(ctx context.Context, req ReadStateRequest) (api.AcceptedStateStatus, error) {
	if s == nil {
		return api.AcceptedStateStatus{}, errors.New("accepted-state reader unavailable")
	}
	result, err := (&AcceptedStatesService{reader: s.accepted}).Get(ctx, GetAcceptedStateRequest(req))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return api.AcceptedStateStatus{}, err
		}
		return api.AcceptedStateStatus{}, errors.New("authenticated accepted-state read unavailable")
	}
	return result, nil
}

// Diff compares accepted state to the proposed configured candidate without\n// promotion. It checks the result's environment,
// layer provenance and revision consistency before releasing any response.
func (s *StateService) Diff(ctx context.Context, req ReadStateRequest) (api.ConfiguredAcceptedDiff, error) {
	if ctx == nil {
		return api.ConfiguredAcceptedDiff{}, errors.New("state diff requires context")
	}
	if err := ctx.Err(); err != nil {
		return api.ConfiguredAcceptedDiff{}, err
	}
	if req.Project == "" || req.EnvironmentID == "" {
		return api.ConfiguredAcceptedDiff{}, errors.New("explicit project and immutable environment ID required")
	}
	if s == nil || s.diff == nil {
		return api.ConfiguredAcceptedDiff{}, errors.New("authenticated state diff unavailable")
	}
	result, err := s.diff.DiffConfiguredAccepted(ctx, req.Project, req.EnvironmentID)
	if err != nil {
		return api.ConfiguredAcceptedDiff{}, errors.New("authenticated state diff unavailable")
	}
	if result.APIVersion != api.APIVersionV1Alpha1 || result.Kind != "ConfiguredAcceptedDiff" ||
		result.FromLayer != "accepted" || result.ToLayer != "configured" ||
		result.Environment.ID != req.EnvironmentID || result.Environment.Binding.Project != req.Project ||
		result.Environment.Binding.ServerID == "" || result.HasAccepted != (result.Revision != nil) ||
		result.NewlySelected == nil || result.Unselected == nil || result.Modified == nil ||
		result.ProposedDigest == "" {
		return api.ConfiguredAcceptedDiff{}, errors.New("state diff returned inconsistent scope or layer")
	}
	if result.Revision != nil && (result.Revision.ID == "" || result.Revision.Generation == 0 || result.Revision.CandidateDigest == "") {
		return api.ConfiguredAcceptedDiff{}, errors.New("state diff returned invalid accepted revision")
	}
	// A drift result must not silently claim "no change" when its
	// component details describe a change (or the inverse).
	anyChanges := result.DisplayNameChanged || len(result.NewlySelected) > 0 ||
		len(result.Unselected) > 0 || len(result.Modified) > 0
	if result.Changed != anyChanges {
		return api.ConfiguredAcceptedDiff{}, errors.New("state diff returned contradictory change summary")
	}
	seen := map[string]bool{}
	for _, id := range result.NewlySelected {
		if id == "" || seen[id] { return api.ConfiguredAcceptedDiff{}, errors.New("state diff returned duplicate workload identity") }
		seen[id] = true
	}
	for _, id := range result.Unselected {
		if id == "" || seen[id] { return api.ConfiguredAcceptedDiff{}, errors.New("state diff returned contradictory workload identity") }
		seen[id] = true
	}
	for _, change := range result.Modified {
		if change.ID == "" || seen[change.ID] || len(change.Fields) == 0 {
			return api.ConfiguredAcceptedDiff{}, errors.New("state diff returned contradictory workload change")
		}
		fields := make(map[string]bool, len(change.Fields))
		for _, field := range change.Fields {
			if field == "" || fields[field] {
				return api.ConfiguredAcceptedDiff{}, errors.New("state diff returned invalid or duplicate field name")
			}
			fields[field] = true
		}
		seen[change.ID] = true
	}
	return result, nil
}
