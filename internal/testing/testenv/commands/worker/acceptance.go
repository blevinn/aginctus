package worker

import (
	"encoding/json"
	"fmt"

	"github.com/blevinn/aginctus/internal/testing/testenv"
	"github.com/spf13/cobra"
)

func NewAcceptance(handlers Handlers) *cobra.Command {
	var cfg testenv.IncusAcceptanceOptions
	cmd := &cobra.Command{
		Use: "acceptance",
		Short: "Run approved disposable-worker lifecycle acceptance",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg.Revision = handlers.Revision
			report, err := handlers.Accept(cmd.Context(), cfg)
			status := "pass"
			stage := ""
			if err != nil {
				status = "fail"
				stage = acceptanceFailureStage(report.FailureStage)
				fmt.Fprintln(cmd.ErrOrStderr(), "worker acceptance incomplete or unverified; inspect private evidence")
			}
			writeErr := json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
				SchemaVersion int    `json:"schema_version"`
				Profile       string `json:"profile"`
				Status        string `json:"status"`
				RunID         string `json:"run_id"`
				FailureStage  string `json:"failure_stage,omitempty"`
			}{1, "dedicated_worker", status, report.RunID, stage})
			if err != nil || writeErr != nil {
				return ErrIncomplete
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
	cmd.Flags().BoolVar(&cfg.RequireNestedKVM, "require-nested-kvm", false, "require the fixed nested KVM guest execution probe")
	cmd.Flags().StringVar(&cfg.Scenario, "scenario", "", "optional fixed scenario: native-instance (requires the reviewed scenario image)")
	cmd.Flags().BoolVar(&cfg.RequirePayloadTransfer, "require-payload-transfer", false, "require a fixed synthetic guest file transfer and exact readback")
	return cmd
}

// Only expose fixed stage codes, never arbitrary errors or private evidence.
func acceptanceFailureStage(stage string) string {
	switch stage {
	case "configuration", "worker_identity", "worker_marker", "worker_credential",
		"report_root", "report_lock", "report_initialize", "evidence", "worker_daemon",
		"preflight", "ephemeral_create", "retained_create", "ephemeral_agent", "retained_agent",
		"ephemeral_status_exec_volume", "retained_status_exec_volume", "nested_kvm_guest_execution",
		"payload_transfer", "native_instance_scenario", "ephemeral_destroy", "scoped_ttl_recovery",
		"foreign_preserved", "cleanup":
		return stage
	default:
		return "unknown"
	}
}
