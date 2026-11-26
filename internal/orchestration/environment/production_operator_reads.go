package environment

import (
	"context"
	"errors"

	"github.com/blevinn/aginctus/pkg/client"
)

// ProductionOperatorReadSet is the capability subset supplied to the ordinary
// installed CLI after independently verifying the restricted Incus TLS
// project. No raw Incus SDK or local project fixture escapes this composition.
type ProductionOperatorReadSet struct {
	Environments client.EnvironmentReader
	Configured client.ConfiguredStateReader
	Accepted client.AcceptedStateReader
	AcceptedSnapshot *AcceptedReadModel
	Diff client.ConfiguredAcceptedDiffReader
	stateSession *RestrictedHTTPSReadSessionFactory
	configuredSource ExplicitConfiguredCandidateFile
}

// OpenProductionOperatorReads opens ONE explicitly selected, independently
// verified project principal. With an optional complete state-service stanza,
// accepted/configured/diff readers are also wired to the pinned mTLS service
// and a strictly selected candidate. No writer or admin capability is given
// to callers. The state service MUST independently verify peer authority and
// revocation: this composition is not proof its deployment is production-safe.
func OpenProductionOperatorReads(
	ctx context.Context, privateConfigPath string,
) (ProductionOperatorReadSet, func(), error) {
	if ctx == nil { return ProductionOperatorReadSet{}, nil, errors.New("project read requires context") }
	if err := ctx.Err(); err != nil { return ProductionOperatorReadSet{}, nil, err }
	config, err := ReadRestrictedOperatorReadConfig(privateConfigPath)
	if err != nil { return ProductionOperatorReadSet{}, nil, errors.New("operator read configuration unavailable") }
	return openProductionOperatorReads(ctx, config)
}

func openProductionOperatorReads(ctx context.Context, config RestrictedOperatorReadConfig) (ProductionOperatorReadSet, func(), error) {
	scoped, err := OpenExplicitRestrictedIncusProject(ctx, config.Connection())
	if err != nil { return ProductionOperatorReadSet{}, nil, errors.New("restricted Incus connection unavailable") }
	catalog := NewRestrictedIncusReadCatalog(scoped, config.TrustedServerSHA256)
	out := ProductionOperatorReadSet{
		Environments: CatalogClientReader{Catalog: catalog},
	}
	if config.StateServiceEndpoint != "" {
		verified := RestrictedIncusProjectVerifier{
			Observer: scoped, PinnedServerCertificateSHA256: config.TrustedServerSHA256,
		}
		factory := RestrictedHTTPSReadSessionFactory{
			Endpoint: config.StateServiceEndpoint,
			ServiceCertificatePEM: config.StateServiceCertificatePEM,
			ServiceCertificateSHA256: config.StateServiceSHA256,
			ClientCertificatePEM: config.ClientCertificatePEM,
			ClientPrivateKeyPEM: config.ClientPrivateKeyPEM,
			Verifier: verified,
		}
		published := RestrictedPublishedSource{Factory: factory}
		configured := ExplicitConfiguredCandidateFile{
			AbsolutePath: config.ConfiguredCandidateFile,
		}
		model := AcceptedReadModel{Catalog: catalog, Source: published}
		out.Configured = VerifiedConfiguredReadModel{Catalog: catalog, Configured: configured}
		out.Accepted = model
		out.AcceptedSnapshot = &model
		out.stateSession = &factory
		out.configuredSource = configured
		out.Diff = VerifiedConfiguredDiffReader{Model: model, Configured: configured}
	}
	if err := ctx.Err(); err != nil {
		scoped.Disconnect()
		return ProductionOperatorReadSet{}, nil, err
	}
	return out, func(){ scoped.Disconnect() }, nil
}
