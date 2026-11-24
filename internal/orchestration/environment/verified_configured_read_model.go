package environment

import (
	"context"
	"errors"

	"github.com/blevinn/aginctus/pkg/api"
)

// VerifiedConfiguredReadModel adapts a selected nonsecret candidate to the
// public configured-state client contract. Both the Catalog and Configured
// source must be independently trusted: this adapter does not authenticate
// Incus credentials, enroll candidates, or promote accepted state.
type VerifiedConfiguredReadModel struct {
	Catalog    Catalog
	Configured ConfiguredCandidateSource
}

// GetConfiguredWorkloads projects only the explicitly selected environment.
// Reverification catches project retargeting during a read but does not
// replace revocation enforcement by the physical authority.
func (r VerifiedConfiguredReadModel) GetConfiguredWorkloads(
	ctx context.Context, project, environmentID string,
) (api.ConfiguredWorkloadList, error) {
	if ctx == nil {
		return api.ConfiguredWorkloadList{}, errors.New("configured-state read requires context")
	}
	if err := ctx.Err(); err != nil {
		return api.ConfiguredWorkloadList{}, err
	}
	if r.Configured == nil || r.Catalog.Verifier == nil || r.Catalog.Source == nil ||
		project == "" || !idPattern.MatchString(environmentID) {
		return api.ConfiguredWorkloadList{}, errors.New("verified configured-state source required")
	}
	enrolled, err := r.Catalog.Get(ctx, project, environmentID)
	if err != nil {
		return api.ConfiguredWorkloadList{}, errors.New("authenticated configured environment unavailable")
	}
	if err := ctx.Err(); err != nil {
		return api.ConfiguredWorkloadList{}, err
	}
	candidate, err := r.Configured.ReadConfigured(ctx, enrolled)
	if err != nil {
		return api.ConfiguredWorkloadList{}, errors.New("configured candidate unavailable")
	}
	result, err := ProjectConfiguredWorkloads(candidate, enrolled, enrolled.Binding)
	if err != nil {
		return api.ConfiguredWorkloadList{}, errors.New("configured candidate does not match enrolled environment")
	}
	if err := ctx.Err(); err != nil {
		return api.ConfiguredWorkloadList{}, err
	}
	binding, err := r.Catalog.Verifier.VerifyProject(ctx, project)
	if err != nil || binding != enrolled.Binding {
		return api.ConfiguredWorkloadList{}, errors.New("project binding changed during configured read")
	}
	if err := ctx.Err(); err != nil {
		return api.ConfiguredWorkloadList{}, err
	}
	return result, nil
}
