package environment

import (
	"context"
	"errors"

	"github.com/blevinn/aginctus/pkg/client"
)

// ProductionEnrollmentReads opens an explicitly configured, independently
// authenticated restricted Incus TLS connection and supplies a read-only
// enrolled-environment client capability. The caller MUST close the returned
// connection after the CLI invocation. This deliberately does not supply
// accepted-state, configured-state, or workload readers: absence is an error,
// not an empty view, until their distinct trusted authorities are available.
func ProductionEnrollmentReads(
	ctx context.Context, privateConfigPath string,
) (client.EnvironmentReader, func(), error) {
	if ctx == nil { return nil, nil, errors.New("restricted project reads require context") }
	if err := ctx.Err(); err != nil { return nil, nil, err }
	config, err := ReadRestrictedOperatorReadConfig(privateConfigPath)
	if err != nil { return nil, nil, errors.New("restricted project read configuration unavailable") }
	scoped, err := OpenExplicitRestrictedIncusProject(ctx, config.Connection())
	if err != nil { return nil, nil, errors.New("restricted project TLS connection unavailable") }
	catalog := NewRestrictedIncusReadCatalog(scoped, config.TrustedServerSHA256)
	return CatalogClientReader{Catalog: catalog}, func(){ scoped.Disconnect() }, nil
}
