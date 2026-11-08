package worker

import (
	"encoding/json"
	"fmt"

	"github.com/blevinn/aginctus/internal/testenv"
	"github.com/spf13/cobra"
)

func NewPreflight(handlers Handlers) *cobra.Command {
	var inspectPool bool
	cmd := &cobra.Command{
		Use: "preflight",
		Short: "Inspect installed dedicated worker image and pool policy",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			report, err := handlers.Preflight(cmd.Context(), testenv.TrustedIncusPreflightOptions{
				SocketPath: handlers.Getenv("AGINCTUS_TESTENV_WORKER_SOCKET"),
				PolicyKeyPath: handlers.Getenv("AGINCTUS_TESTENV_POLICY_KEY_FILE"),
				ImageFingerprint: handlers.Getenv("AGINCTUS_TESTENV_IMAGE_FINGERPRINT"),
				PoolConfigFingerprint: handlers.Getenv("AGINCTUS_TESTENV_POOL_CONFIG_FINGERPRINT"),
				InspectPool: inspectPool,
			})
			if err != nil {
				report.Status = "fail"
			}
			report.SchemaVersion, report.Profile = 1, "worker_preflight"
			writeErr := json.NewEncoder(cmd.OutOrStdout()).Encode(report)
			if err != nil || writeErr != nil {
				// Never include daemon diagnostic text or the worker key.
				fmt.Fprintln(cmd.ErrOrStderr(), "worker preflight incomplete or unverified")
				return ErrIncomplete
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&inspectPool, "pool-fingerprint", false, "Inspect the worker pool fingerprint without admission")
	return cmd
}

