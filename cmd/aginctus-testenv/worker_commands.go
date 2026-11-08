package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/blevinn/aginctus/internal/testenv"
	"github.com/spf13/cobra"
)

// These privileged operator-only leaves are deliberately not registered by
// the normal aginctus CLI or MCP server. A dedicated worker and its installed
// service credentials remain prerequisites for any live Incus operation.
type workerServices struct {
	getenv    func(string) string
	preflight func(context.Context, testenv.TrustedIncusPreflightOptions) (testenv.IncusWorkerPreflightReport, error)
	accept    func(context.Context, testenv.IncusAcceptanceOptions) (testenv.IncusAcceptanceReport, error)
	sweep     func(context.Context, testenv.TrustedIncusSweepOptions, time.Time) (testenv.IncusReconcileResult, error)
	now       func() time.Time
}

var revision = "source-unverified"

func defaultWorkerServices() workerServices {
	return workerServices{
		getenv: os.Getenv,
		preflight: testenv.RunTrustedIncusWorkerPreflight,
		accept: testenv.RunTrustedIncusAcceptance,
		sweep: testenv.RunTrustedIncusExpirySweep,
		now: time.Now,
	}
}

func newPreflightCommand(services workerServices) *cobra.Command {
	var inspectPool bool
	cmd := &cobra.Command{
		Use: "preflight",
		Short: "Inspect installed dedicated worker image and pool policy",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			report, err := services.preflight(cmd.Context(), testenv.TrustedIncusPreflightOptions{
				SocketPath: services.getenv("AGINCTUS_TESTENV_WORKER_SOCKET"),
				PolicyKeyPath: services.getenv("AGINCTUS_TESTENV_POLICY_KEY_FILE"),
				ImageFingerprint: services.getenv("AGINCTUS_TESTENV_IMAGE_FINGERPRINT"),
				PoolConfigFingerprint: services.getenv("AGINCTUS_TESTENV_POOL_CONFIG_FINGERPRINT"),
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
				return errWorkerIncomplete
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&inspectPool, "pool-fingerprint", false, "Inspect the worker pool fingerprint without admission")
	return cmd
}

func newAcceptanceCommand(services workerServices) *cobra.Command {
	var cfg testenv.IncusAcceptanceOptions
	cmd := &cobra.Command{
		Use: "acceptance",
		Short: "Run approved disposable-worker lifecycle acceptance",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg.Revision = revision
			report, err := services.accept(cmd.Context(), cfg)
			status := "pass"
			if err != nil {
				status = "fail"
				fmt.Fprintln(cmd.ErrOrStderr(), "worker acceptance incomplete or unverified; inspect private evidence")
			}
			writeErr := json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
				SchemaVersion int    `json:"schema_version"`
				Profile       string `json:"profile"`
				Status        string `json:"status"`
				RunID         string `json:"run_id"`
			}{1, "dedicated_worker", status, report.RunID})
			if err != nil || writeErr != nil {
				return errWorkerIncomplete
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.Acknowledgement, "disposable-worker", "", "required: DEDICATED_DISPOSABLE_WORKER (not isolation proof)")
	cmd.Flags().StringVar(&cfg.SocketPath, "socket", "", "required: dedicated /var/lib/incus/unix.socket")
	cmd.Flags().StringVar(&cfg.Project, "project", "", "required: aginctus-testenv")
	cmd.Flags().StringVar(&cfg.ImageFingerprint, "image", "", "required: immutable 64-hex imported image fingerprint")
	cmd.Flags().StringVar(&cfg.PolicyKeyPath, "policy-key-file", "", "required: private runtime worker credential")
	cmd.Flags().StringVar(&cfg.ReportRoot, "report-root", "", "required: existing worker-owned 0700 evidence directory")
	cmd.Flags().StringVar(&cfg.ForeignInstance, "foreign-instance", "", "required: existing unsigned foreign VM sentinel")
	return cmd
}

func newSweepCommand(services workerServices) *cobra.Command {
	return &cobra.Command{
		Use: "sweep",
		Short: "Reconcile expired signed VMs on the installed dedicated worker",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options := testenv.TrustedIncusSweepOptions{
				SocketPath: services.getenv("AGINCTUS_TESTENV_WORKER_SOCKET"),
				StoragePool: services.getenv("AGINCTUS_TESTENV_WORKER_POOL"),
				PolicyKeyPath: services.getenv("AGINCTUS_TESTENV_POLICY_KEY_FILE"),
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 120*time.Second)
			defer cancel()
			result, err := services.sweep(ctx, options, services.now().UTC())
			fmt.Fprintf(cmd.OutOrStdout(), "testenv-recovery scanned=%d skipped=%d destroyed=%d failed=%d\n",
				result.Scanned, result.Skipped, result.Destroyed, result.Failed)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "testenv recovery: incomplete or unverified")
				return errWorkerIncomplete
			}
			return nil
		},
	}
}
