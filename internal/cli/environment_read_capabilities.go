package cli

import (
	"errors"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
)

// NewEnvironmentReadCapabilities composes the existing domain read models for
// a trusted embedding host. It does not discover credentials or authenticate
// a caller. The supplied Catalog and PublishedAcceptedSource MUST be backed by
// independently authenticated, project-restricted authority before exposing
// this bundle to an operator. In particular the fixture Unix service is NOT
// an acceptable production PublishedAcceptedSource.
//
// Logical workloads require a separate environment-owned authoritative
// reader; leaving Workloads nil deliberately fails closed.
func NewEnvironmentReadCapabilities(
	catalog environment.Catalog,
	published environment.PublishedAcceptedSource,
	configured environment.ConfiguredCandidateSource,
) (ReadCapabilities, error) {
	if catalog.Verifier == nil || catalog.Source == nil || published == nil || configured == nil {
		return ReadCapabilities{}, errors.New("authenticated environment, published, and configured sources required")
	}
	accepted := environment.AcceptedReadModel{Catalog: catalog, Source: published}
	return ReadCapabilities{
		Environments: environment.CatalogClientReader{Catalog: catalog},
		Configured:   environment.VerifiedConfiguredReadModel{Catalog: catalog, Configured: configured},
		Accepted:     accepted,
		Diff: environment.VerifiedConfiguredDiffReader{
			Model: accepted, Configured: configured,
		},
	}, nil
}
