package cli

import (
	"context"
	"io"

	"github.com/blevinn/aginctus/internal/config"
	workloadcmd "github.com/blevinn/aginctus/internal/cli/commands/workload"
	"github.com/spf13/cobra"
)

// normalizeWorkloadLegacyArgs supports the old "workload ID ensure|teardown"
// grammar without falling back into the old positional dispatcher.
func normalizeWorkloadLegacyArgs(args []string) []string {
	if len(args) < 3 || args[0] != "workload" {
		return args
	}
	switch args[1] {
	case "apply", "remove", "start", "stop", "status", "ls", "list", "gateway":
		return args
	}
	var action string
	switch args[2] {
	case "ensure":
		action = "apply"
	case "teardown":
		action = "remove"
	default:
		return args
	}
	mapped := append([]string{"workload", action, args[1]}, args[3:]...)
	return mapped
}

func addWorkloadCommands(root *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int,
) {
	workloadcmd.Register(root, stdout, stderr, load, status, workloadcmd.Handlers{
		ScopedApply: runScopedWorkloadApplyRender,
		ScopedStart: runScopedWorkloadStart,
		ScopedStop: runScopedWorkloadStop,
		Configured: func(ctx context.Context, action string, args []string, dryRun, force bool, out, errOut io.Writer, cfg *config.Config) int {
			return executeConfiguredWorkloads(ctx, action, args, mutationOptions{DryRun: dryRun, Force: force}, out, errOut, cfg)
		},
		ScopedStatus: runScopedWorkloadStatus,
		List: runActiveWorkloadList,
		Status: runWorkloadStatus,
		GatewayProvision: runWorkloadGatewayProvision,
	})
}
