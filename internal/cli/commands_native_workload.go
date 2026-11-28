package cli

import (
	"errors"
	"io"
	"os"
	"time"

	clioutput "github.com/blevinn/aginctus/internal/cli/output"
	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/nativelifecycle"
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/native"
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
	var workload, revision, grant, operation, createdAt string
	var herdr, bootstrap, discardRoot bool
	var volumeCreatedAt string
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
			var incarnation time.Time
			if createdAt != "" {
				incarnation, err = time.Parse(time.RFC3339Nano, createdAt)
				if err != nil || incarnation.IsZero() {
					return api.NewError(api.CodeInvalidArgument, "instance creation timestamp must be RFC3339")
				}
			}
			if herdr {
				if workload != "" {
					return errors.New("select Herdr alone")
				}
				var result orchestration.Result
				var op string
				if operation == "reconcile" {
					if createdAt != "" || volumeCreatedAt != "" || discardRoot {
						return errors.New("teardown flags require a Herdr stop or removal")
					}
					result, op, err = nativelifecycle.ExecuteInstalledHerdr(cmd.Context(), config, grant, *project, *id, revision, bootstrap, false)
				} else {
					if bootstrap || (operation != "stop" && operation != "remove") || discardRoot != (operation == "remove") || incarnation.IsZero() {
						return errors.New("herdr teardown requires exact instance and volume timestamps, separate operation, and explicit root discard for removal")
					}
					volumeAt, parseErr := time.Parse(time.RFC3339Nano, volumeCreatedAt)
					if parseErr != nil || volumeAt.IsZero() {
						return errors.New("herdr state volume creation timestamp must be RFC3339")
					}
					result, op, err = nativelifecycle.ExecuteInstalledHerdrTeardown(cmd.Context(), config, grant, *project, *id, revision, native.ProjectInstanceOperation(operation), incarnation, volumeAt)
				}
				if op == "" && err != nil {
					return err
				}
				return errors.Join(err, clioutput.WriteStructured(out, f, workloadReconcileDocument{TypeMeta: api.TypeMeta{APIVersion: "aginctus.dev/v1alpha1", Kind: "NativeHerdrResult"}, OperationID: op, Result: result}))
			}
			if workload == "" || bootstrap || discardRoot || volumeCreatedAt != "" {
				return errors.New("select one workload or Herdr; bootstrap applies only to Herdr")
			}
			result, op, err := nativelifecycle.ExecuteInstalledWorkload(cmd.Context(), config, grant, *project, *id, revision, workload, native.ProjectInstanceOperation(operation), incarnation)
			if op == "" && err != nil {
				return err
			}
			return errors.Join(err, clioutput.WriteStructured(out, f, workloadReconcileDocument{
				TypeMeta: api.TypeMeta{APIVersion: "aginctus.dev/v1alpha1", Kind: "NativeWorkloadResult"}, OperationID: op, Result: result,
			}))
		},
	}
	cmd.Flags().BoolVar(&discardRoot, "discard-herdr-root", false, "Explicitly discard only the stopped Herdr root filesystem on removal")
	cmd.Flags().StringVar(&volumeCreatedAt, "herdr-state-created-at", "", "Exact retained Herdr state volume creation timestamp (RFC3339)")
	cmd.Flags().BoolVar(&herdr, "herdr", false, "Select accepted Herdr instance and retained state")
	cmd.Flags().BoolVar(&bootstrap, "bootstrap-herdr-state", false, "Separately authorized initial Herdr state creation")
	cmd.Flags().StringVar(&workload, "workload", "", "Exact accepted workload ID")
	cmd.Flags().StringVar(&revision, "expect", "", "Exact accepted revision ID")
	cmd.Flags().StringVar(&grant, "host-grant", "", "Independently installed administrator-owned attachment grant")
	cmd.Flags().StringVar(&operation, "operation", "reconcile", "Lifecycle action: reconcile, stop, or remove (requires its own grant)")
	cmd.Flags().StringVar(&createdAt, "instance-created-at", "", "Expected live instance creation timestamp (RFC3339; required for stop or remove)")
	for _, flag := range []string{"expect", "host-grant"} {
		_ = cmd.MarkFlagRequired(flag)
	}
	state.AddCommand(cmd)
}
