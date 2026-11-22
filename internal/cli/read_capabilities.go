package cli

import (
	"context"
	"io"
	"os"

	gatewayops "github.com/blevinn/aginctus/internal/cli/operations/gateway"
	"github.com/blevinn/aginctus/internal/plugins"
	"github.com/blevinn/aginctus/pkg/client"
)

// ReadCapabilities collects separately authenticated, read-only in-process
// domain capabilities for a single CLI invocation. Possessing this struct
// does not authenticate an Incus project; its concrete readers must prove
// project authority independently for each request.
//
// In particular, no reader is populated from the test-only same-UID state
// socket or inferred from configuration, labels, or an Incus project flag.
type ReadCapabilities struct {
	Environments client.EnvironmentReader
	Workloads    client.LogicalWorkloadReader
	Configured   client.ConfiguredStateReader
	Accepted     client.AcceptedStateReader
	Diff         client.ConfiguredAcceptedDiffReader
}

// RunWithReadCapabilities exposes the complete environment/workload/state
// read command surface to one trusted embedding host without replacing any
// process-wide hooks. Nil capabilities always fail closed in their commands.
// The standalone Run entrypoint intentionally does not populate this set.
func RunWithReadCapabilities(
	ctx context.Context, args []string, stdout, stderr io.Writer,
	incusClient IncusClient, configLoader ConfigLoader, registry *plugins.Registry,
	readers ReadCapabilities,
) int {
	return runBasicCobraWithStateCapabilities(
		ctx, args, stdout, stderr, incusClient, configLoader, registry,
		readers.Workloads, readers.Environments, os.Stdin, gatewayops.QueryIncusEgress,
		readers.Configured, readers.Accepted, readers.Diff,
	)
}
