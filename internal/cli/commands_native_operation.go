package cli

import (
	"errors"
	"io"
	"os"

	clioutput "github.com/blevinn/aginctus/internal/cli/output"
	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/pkg/api"
	"github.com/blevinn/aginctus/pkg/client"
	"github.com/spf13/cobra"
)

type nativeOperationDocument struct {
	api.TypeMeta `json:",inline"`
	Operations []environment.ProjectOperationSummary `json:"operations"`
}

func registerNativeOperationInspection(state *cobra.Command, out io.Writer, format func() (clioutput.Format, error), project, id *string) {
	var workload, revision string
	var recoverOperation bool
	cmd := &cobra.Command{
		Use: "operations [OPERATION_ID]", Short: "Inspect authorized native workload journal history", Hidden: true,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := format()
			if err != nil {
				return err
			}
			if f != clioutput.JSON {
				return errors.New("state operations requires json output")
			}
			if _, err := incusscope.NewProjectScope(*project); err != nil || *project == "default" || !client.IsEnvironmentUUID(*id) {
				return errors.New("state operations requires an explicit nondefault project and environment UUID")
			}
			config, err := environment.ReadRestrictedOperatorReadConfig(os.Getenv("AGINCTUS_RESTRICTED_READ_CONFIG"))
			if err != nil {
				return err
			}
			var operationID string
			if len(args) != 0 {
				operationID = args[0]
				if operationID == "" {
					return errors.New("empty operation ID")
				}
			}
			if recoverOperation {
				if operationID == "" {
					return errors.New("recovery requires one exact operation ID")
				}
				if err := environment.RecoverProjectOperation(cmd.Context(), config, *project, *id, revision, workload, operationID); err != nil {
					return err
				}
				return clioutput.WriteStructured(out, f, struct {
					OperationID string `json:"operationId"`
					Recovered bool `json:"recovered"`
				}{operationID, true})
			}
			result, err := environment.InspectProjectOperations(cmd.Context(), config, *project, *id, revision, workload, operationID)
			if err != nil {
				return err
			}
			return clioutput.WriteStructured(out, f, nativeOperationDocument{
				TypeMeta: api.TypeMeta{APIVersion: "aginctus.dev/v1alpha1", Kind: "NativeOperationInspection"}, Operations: result,
			})
		},
	}
	cmd.Flags().BoolVar(&recoverOperation, "recover", false, "Resolve positively observed terminal reconcile tasks with a separate recovery grant")
	cmd.Flags().StringVar(&workload, "workload", "", "Exact accepted workload ID")
	cmd.Flags().StringVar(&revision, "revision", "", "Exact accepted revision ID, current or historical")
	_ = cmd.MarkFlagRequired("workload")
	_ = cmd.MarkFlagRequired("revision")
	state.AddCommand(cmd)
}
