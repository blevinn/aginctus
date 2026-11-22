package client

import (
	"context"
	"errors"

	"github.com/blevinn/aginctus/pkg/api"
)

// PlanAcceptanceRequest is an explicit scoped read request. A plan is an
// advisory snapshot, not a credential, CAS authorization or mutation command.
type PlanAcceptanceRequest = ReadStateRequest

// PlanAcceptance obtains the same verified accepted-to-configured comparison
// as State.Diff and projects it into a deterministic public acceptance plan.
// The execution service must independently revalidate authority, candidate
// identity and the current accepted revision at commit time.
func (s *StateService) PlanAcceptance(ctx context.Context, req PlanAcceptanceRequest) (api.AcceptancePlan, error) {
	diff, err := s.Diff(ctx, ReadStateRequest(req))
	if err != nil {
		return api.AcceptancePlan{}, err
	}
	plan, err := api.PlanAcceptanceFromDiff(diff)
	if err != nil {
		return api.AcceptancePlan{}, errors.New("accepted-to-configured comparison cannot be planned")
	}
	if plan.EnvironmentID != req.EnvironmentID || plan.Project != req.Project {
		return api.AcceptancePlan{}, errors.New("acceptance plan scope mismatch")
	}
	if err := ctx.Err(); err != nil {
		return api.AcceptancePlan{}, err
	}
	return plan, nil
}
