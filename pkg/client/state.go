package client

import (
	"context"
	"errors"

	"github.com/blevinn/aginctus/pkg/api"
)

// ConfiguredStateReader is a capability supplied by an independently verified
// environment candidate selection. A raw candidate file or project name alone
// cannot satisfy the authentication/ownership requirements of this interface.
type ConfiguredStateReader interface {
	GetConfiguredWorkloads(context.Context, string, string) (api.ConfiguredWorkloadList, error)
}

// StateService currently exposes only configured-layer read projections.
// Accepted and observed state remain separate authorities and will be added
// after the project-scoped service and reconciliation contracts are validated.
type StateService struct {
	configured ConfiguredStateReader
	accepted AcceptedStateReader
	diff ConfiguredAcceptedDiffReader
}

// GetConfiguredStateRequest selects an environment by immutable identity and
// exact Incus project; it does not authorize a write or adopt any resource.
type GetConfiguredStateRequest struct {
	Project       string
	EnvironmentID string
}

// Configured rejects missing authority, foreign data and malformed responses.
// It never treats a failed lookup as an empty configured-state document.
func (s *StateService) Configured(ctx context.Context, req GetConfiguredStateRequest) (api.ConfiguredWorkloadList, error) {
	if ctx == nil {
		return api.ConfiguredWorkloadList{}, errors.New("configured-state read requires context")
	}
	if err := ctx.Err(); err != nil {
		return api.ConfiguredWorkloadList{}, err
	}
	if req.Project == "" || req.EnvironmentID == "" {
		return api.ConfiguredWorkloadList{}, errors.New("explicit project and immutable environment ID required")
	}
	if s == nil || s.configured == nil {
		return api.ConfiguredWorkloadList{}, errors.New("verified configured-state reader unavailable")
	}
	result, err := s.configured.GetConfiguredWorkloads(ctx, req.Project, req.EnvironmentID)
	if err != nil {
		return api.ConfiguredWorkloadList{}, errors.New("configured-state read unavailable")
	}
	if result.APIVersion != api.APIVersionV1Alpha1 || result.Kind != "ConfiguredWorkloadList" ||
		result.Layer != "configured" || result.EnvironmentID != req.EnvironmentID || result.Items == nil {
		return api.ConfiguredWorkloadList{}, errors.New("configured-state reader returned inconsistent scope")
	}
	seen := make(map[string]bool, len(result.Items))
	for _, item := range result.Items {
		if item.ID == "" || seen[item.ID] || item.Profiles == nil {
			return api.ConfiguredWorkloadList{}, errors.New("configured-state reader returned invalid workload")
		}
		seen[item.ID] = true
	}
	return result, nil
}
