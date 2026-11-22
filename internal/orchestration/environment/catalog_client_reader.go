package environment

import (
	"context"
	"errors"

	"github.com/blevinn/aginctus/pkg/api"
)

// CatalogClientReader exposes the existing authenticated enrollment Catalog to
// pkg/client without introducing a second enrollment implementation. Its
// Verifier and Source MUST independently enforce the restricted project
// principal; constructing this adapter does not establish authorization.
//
// In particular, do not populate this with candidate files, instance labels,
// the local same-UID fixture, or an unrestricted Incus administrator session.
type CatalogClientReader struct {
	Catalog Catalog
}

func (r CatalogClientReader) ListEnvironments(ctx context.Context, project string) (api.EnvironmentList, error) {
	if ctx == nil {
		return api.EnvironmentList{}, errors.New("environment list requires context")
	}
	descriptors, err := r.Catalog.List(ctx, project)
	if err != nil {
		return api.EnvironmentList{}, err
	}
	items := make([]api.Environment, 0, len(descriptors))
	for _, descriptor := range descriptors {
		items = append(items, projectedEnvironment(descriptor))
	}
	if err := ctx.Err(); err != nil {
		return api.EnvironmentList{}, err
	}
	return api.EnvironmentList{TypeMeta: api.NewTypeMeta("EnvironmentList"), Items: items}, nil
}

func (r CatalogClientReader) GetEnvironment(ctx context.Context, project, id string) (api.EnvironmentDetail, error) {
	if ctx == nil {
		return api.EnvironmentDetail{}, errors.New("environment lookup requires context")
	}
	descriptor, err := r.Catalog.Get(ctx, project, id)
	if err != nil {
		return api.EnvironmentDetail{}, err
	}
	if err := ctx.Err(); err != nil {
		return api.EnvironmentDetail{}, err
	}
	return api.EnvironmentDetail{TypeMeta: api.NewTypeMeta("Environment"), Environment: projectedEnvironment(descriptor)}, nil
}
