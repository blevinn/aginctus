package cli

import (
	"context"
	"io"

	egresscmd "github.com/blevinn/aginctus/internal/cli/commands/gateway/egress"
	gatewayops "github.com/blevinn/aginctus/internal/cli/operations/gateway"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

// Keep an injectable read-only Incus query for network/firewall and gateway tests.
var gatewayEgressQuery = gatewayops.QueryIncusEgress

func executeGatewayEgress(ctx context.Context, action, zone, ranges string,
	stdout, stderr io.Writer, effective *config.Config,
) int {
	return gatewayops.ExecuteEgress(ctx, action, zone, ranges, stdout, stderr, effective, gatewayEgressQuery)
}

func addGatewayEgressCommands(
	gatewayCmd *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int,
) {
	egresscmd.Register(gatewayCmd, stdout, stderr, load, status, egresscmd.Handlers{
		Egress: executeGatewayEgress,
		Verify: verifyGatewayResponsesStream,
	})
}
