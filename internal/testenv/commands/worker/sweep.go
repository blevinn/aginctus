package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/blevinn/aginctus/internal/testenv"
	"github.com/spf13/cobra"
)

func NewSweep(handlers Handlers) *cobra.Command {
	return &cobra.Command{
		Use: "sweep",
		Short: "Reconcile expired signed VMs on the installed dedicated worker",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options := testenv.TrustedIncusSweepOptions{
				SocketPath: handlers.Getenv("AGINCTUS_TESTENV_WORKER_SOCKET"),
				StoragePool: handlers.Getenv("AGINCTUS_TESTENV_WORKER_POOL"),
				PolicyKeyPath: handlers.Getenv("AGINCTUS_TESTENV_POLICY_KEY_FILE"),
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 120*time.Second)
			defer cancel()
			result, err := handlers.Sweep(ctx, options, handlers.Now().UTC())
			fmt.Fprintf(cmd.OutOrStdout(), "testenv-recovery scanned=%d skipped=%d destroyed=%d failed=%d\n",
				result.Scanned, result.Skipped, result.Destroyed, result.Failed)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "testenv recovery: incomplete or unverified")
				return ErrIncomplete
			}
			return nil
		},
	}
}
