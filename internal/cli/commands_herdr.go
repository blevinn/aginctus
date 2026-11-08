package cli

import (
	"context"
	"io"

	herdrcmd "github.com/blevinn/aginctus/internal/cli/commands/herdr"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

func addHerdrCommands(root *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int,
) {
	herdrcmd.Register(root, stdout, stderr, load, status, herdrcmd.Handlers{
		Console: runHerdrConsole,
		Authorize: runHerdrAuthorize,
		PinHost: runHerdrPinHost,
		RegisterWorkload: runHerdrRegister,
		Mutation: func(ctx context.Context, action string, dryRun, force bool, out, errOut io.Writer, cfg *config.Config) int {
			return executeHerdrClientOperation(ctx, action, mutationOptions{DryRun: dryRun, Force: force}, out, errOut, cfg)
		},
		Transition: transitionHerdrClientCommand,
		ClientStatus: runHerdrClientStatus,
		ScopedClientStatus: runScopedHerdrClientStatus,
	})
}
