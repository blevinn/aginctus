package cli

import (
	"errors"
	"io"
	"os"

	clioutput "github.com/blevinn/aginctus/internal/cli/output"
	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/nativelifecycle"
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/pkg/api"
	"github.com/blevinn/aginctus/pkg/client"
	"github.com/spf13/cobra"
)

type workloadReconcileDocument struct {
	api.TypeMeta `json:",inline" yaml:",inline"`
	OperationID  string               `json:"operationId" yaml:"operationId"`
	Result       orchestration.Result `json:"result" yaml:"result"`
}

func registerNativeWorkloadReconcile(state *cobra.Command, out io.Writer, format func() (clioutput.Format, error), project, id *string) {
	var workload, revision, grant string
	cmd := &cobra.Command{
		Use: "reconcile", Short: "Reconcile one explicitly selected accepted native workload", Hidden: true,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f, err := format()
			if err != nil {
				return err
			}
			if f != clioutput.JSON && f != clioutput.YAML {
				return errors.New("state reconcile requires json or yaml output")
			}
			if _, err := incusscope.NewProjectScope(*project); err != nil || !client.IsEnvironmentUUID(*id) {
				return errors.New("state reconcile requires an explicit nondefault project and environment UUID")
			}
			config := os.Getenv("AGINCTUS_RESTRICTED_READ_CONFIG")
			if config == "" {
				return errors.New("state reconcile requires AGINCTUS_RESTRICTED_READ_CONFIG")
			}
			result, op, err := nativelifecycle.ReconcileInstalledWorkload(cmd.Context(), config, grant, *project, *id, revision, workload)
			if op == "" && err != nil {
				return err
			}
			return errors.Join(err, clioutput.WriteStructured(out, f, workloadReconcileDocument{
				TypeMeta: api.TypeMeta{APIVersion: "aginctus.dev/v1alpha1", Kind: "NativeWorkloadResult"}, OperationID: op, Result: result,
			}))
		},
	}
	cmd.Flags().StringVar(&workload, "workload", "", "Exact accepted workload ID")
	cmd.Flags().StringVar(&revision, "expect", "", "Exact accepted revision ID")
	cmd.Flags().StringVar(&grant, "host-grant", "", "Independently installed administrator-owned attachment grant")
	for _, flag := range []string{"workload", "expect", "host-grant"} {
		_ = cmd.MarkFlagRequired(flag)
	}
	state.AddCommand(cmd)
}
