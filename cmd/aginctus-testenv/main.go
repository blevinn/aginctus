// aginctus-testenv is a separate, operator-only test environment tool.
// Its worker actions are never registered in the public aginctus CLI or MCP.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	workercmd "github.com/blevinn/aginctus/internal/testenv/commands/worker"
	"github.com/spf13/cobra"
)

// Keep the test fixtures' planner input contract in the command package.
type plannerInput = workercmd.PlannerInput

func newRootCommand(stdin io.Reader, stdout, stderr io.Writer) *cobra.Command {
	return newRootCommandWithServices(stdin, stdout, stderr, defaultWorkerServices())
}

func newRootCommandWithServices(stdin io.Reader, stdout, stderr io.Writer, services workerServices) *cobra.Command {
	root := &cobra.Command{
		Use: "aginctus-testenv",
		Short: "Operator tools for isolated test environments",
		SilenceErrors: true,
		SilenceUsage: true,
	}
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(workercmd.New(services.handlers()))
	return root
}

func execute(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return executeWithServices(context.Background(), args, stdin, stdout, stderr, defaultWorkerServices())
}

func executeWithServices(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, services workerServices) int {
	root := newRootCommandWithServices(stdin, stdout, stderr, services)
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(stderr, err)
		if errors.Is(err, workercmd.ErrOperatorAction) || errors.Is(err, workercmd.ErrIncomplete) {
			return 1
		}
		return 2
	}
	return 0
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(executeWithServices(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, defaultWorkerServices()))
}
