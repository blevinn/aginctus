package client

import (
	"context"
	"errors"
	"fmt"

	"github.com/blevinn/aginctus/pkg/api"
)

// AcceptedStateReader is a read-only capability supplied by an independently
// authenticated Incus project state authority. Never use local file paths,
// caller-provided project labels or an administrator socket as authority.
type AcceptedStateReader interface {
	GetAcceptedState(context.Context, string, string) (api.AcceptedStateStatus, error)
}

// AcceptedStatesService is a read-only typed in-process client facade.
// It cannot accept revisions, reconcile workloads or create a state service.
type AcceptedStatesService struct {
	reader AcceptedStateReader
}

type GetAcceptedStateRequest struct {
	Project       string
	EnvironmentID string
}

// Get checks the request before invoking the injected authenticated reader,
// and rejects incorrectly scoped, malformed or self-contradictory results.
func (s *AcceptedStatesService) Get(ctx context.Context, req GetAcceptedStateRequest) (api.AcceptedStateStatus, error) {
	if ctx == nil {
		return api.AcceptedStateStatus{}, errors.New("accepted-state read requires context")
	}
	if err := ctx.Err(); err != nil {
		return api.AcceptedStateStatus{}, err
	}
	if req.Project == "" || req.EnvironmentID == "" {
		return api.AcceptedStateStatus{}, errors.New("explicit project and immutable environment ID required")
	}
	if s == nil || s.reader == nil {
		return api.AcceptedStateStatus{}, errors.New("authenticated accepted-state reader unavailable")
	}
	response, err := s.reader.GetAcceptedState(ctx, req.Project, req.EnvironmentID)
	if err != nil {
		// Never treat access denied, backend unavailable or inconsistent
		// state as an empty accepted revision.
		return api.AcceptedStateStatus{}, fmt.Errorf("accepted-state read unavailable: %w", err)
	}
	if response.APIVersion != api.APIVersionV1Alpha1 || response.Kind != "AcceptedStateStatus" ||
		response.Layer != "accepted" ||
		response.Environment.ID != req.EnvironmentID ||
		response.Environment.Binding.Project != req.Project ||
		response.Environment.Binding.ServerID == "" ||
		response.Environment.Name == "" {
		return api.AcceptedStateStatus{}, errors.New("accepted-state reader returned inconsistent environment scope")
	}
	if response.HasAccepted != (response.Revision != nil) {
		return api.AcceptedStateStatus{}, errors.New("accepted-state reader returned inconsistent active pointer")
	}
	if response.Revision != nil && (response.Revision.ID == "" ||
		response.Revision.Generation == 0 || response.Revision.CandidateDigest == "") {
		return api.AcceptedStateStatus{}, errors.New("accepted-state reader returned incomplete revision")
	}
	return response, nil
}
