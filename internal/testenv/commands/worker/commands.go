package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/blevinn/aginctus/internal/testenv"
	"github.com/spf13/cobra"
)

// Handlers are provided only by the standalone operator binary, never by
// the aginctus public CLI or MCP server. No leaf discovers worker credentials
// or an Incus daemon until Cobra has validated its arguments.
type Handlers struct {
	Getenv func(string) string
	Preflight func(context.Context, testenv.TrustedIncusPreflightOptions) (testenv.IncusWorkerPreflightReport, error)
	Accept func(context.Context, testenv.IncusAcceptanceOptions) (testenv.IncusAcceptanceReport, error)
	Sweep func(context.Context, testenv.TrustedIncusSweepOptions, time.Time) (testenv.IncusReconcileResult, error)
	Now func() time.Time
	Revision string
}

var ErrIncomplete = errors.New("worker operation incomplete or unverified")

func groupRun(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s requires a subcommand", cmd.CommandPath())
	}
	return fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())
}

// New constructs the operator worker command hierarchy per invocation.
func New(h Handlers) *cobra.Command {
	root := &cobra.Command{
		Use: "worker", Short: "Inspect or prepare a dedicated test environment worker",
		RunE: groupRun,
	}
	root.AddCommand(newBootstrap(), NewPreflight(h), NewAcceptance(h), NewSweep(h))
	return root
}
