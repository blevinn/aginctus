package cli

import (
	"context"
	"io"

	networkcmd "github.com/blevinn/aginctus/internal/cli/commands/network"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

// Bind the independent Cobra parser to the existing network implementation.
func addNetworkCommands(root *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int,
) {
	networkcmd.Register(root, stdout, stderr, load, status, networkcmd.Handlers{
		Operation: func(ctx context.Context, operation string, dryRun, force bool, out, errOut io.Writer, cfg *config.Config) int {
			return executeNetworkOperation(ctx, operation, mutationOptions{DryRun: dryRun, Force: force}, out, errOut, cfg)
		},
		Status: func(ctx context.Context, out, errOut io.Writer, cfg *config.Config) int {
			return runNetworkStatus(ctx, nil, out, errOut, cfg)
		},
		FirewallPolicy: runNetworkFirewallPolicy,
		FirewallReconcile: runNetworkFirewallReconcile,
		FirewallEgress: runNetworkFirewallEgress,
	})
}
