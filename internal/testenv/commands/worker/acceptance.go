package worker

import (
	"encoding/json"
	"fmt"

	"github.com/blevinn/aginctus/internal/testenv"
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
	return cmd
}

