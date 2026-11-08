package cli

import (
	"context"
	"io"

	aggregatecmd "github.com/blevinn/aginctus/internal/cli/commands/aggregate"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

// Root-level aggregate verbs are registered separately from domain logic.
func addAggregateCommands(root *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int,
) {
	aggregatecmd.Register(root, stdout, stderr, load, status, aggregatecmd.Handlers{
		Apply: executeAggregateApply,
		Remove: func(ctx context.Context, out, errOut io.Writer, cfg *config.Config) int {
			return runAggregateRemove(ctx, nil, out, errOut, cfg)
		},
		Status: func(ctx context.Context, out, errOut io.Writer, cfg *config.Config) int {
			return runAggregateStatus(ctx, nil, out, errOut, cfg)
		},
	})
}
