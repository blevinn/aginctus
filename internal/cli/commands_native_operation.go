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
	Operations   []environment.ProjectOperationSummary `json:"operations"`
}

type nativeOperationRecoveryDocument struct {
	api.TypeMeta `json:",inline"`
	OperationID  string `json:"operationId"`
	Recovered    bool   `json:"recovered"`
}

func registerNativeOperationInspection(state *cobra.Command, out io.Writer, format func() (clioutput.Format, error), project, id *string) {
	var workload, revision string
	var recoverOperation, herdr, gateway bool
	cmd := &cobra.Command{
		Use: "operations [OPERATION_ID]", Short: "Inspect authorized native component or workload journal history", Hidden: true,
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
			target, inspection, err := nativeOperationSelection(workload, herdr, gateway, recoverOperation)
			if err != nil {
				return err
			}
			if recoverOperation {
				if operationID == "" {
					return errors.New("recovery requires one exact operation ID")
				}
				if err := environment.RecoverProjectOperation(cmd.Context(), config, *project, *id, revision, target, operationID); err != nil {
					return err
				}
				return clioutput.WriteStructured(out, f, nativeOperationRecoveryDocument{
					TypeMeta:    api.TypeMeta{APIVersion: "aginctus.dev/v1alpha1", Kind: "NativeOperationRecovery"},
					OperationID: operationID, Recovered: true,
				})
			}
			result, err := environment.InspectProjectDomainOperations(cmd.Context(), config, *project, *id, revision, target, operationID, inspection)
			if err != nil {
				return err
			}
			return clioutput.WriteStructured(out, f, nativeOperationDocument{
				TypeMeta: api.TypeMeta{APIVersion: "aginctus.dev/v1alpha1", Kind: "NativeOperationInspection"}, Operations: result,
			})
		},
	}
	cmd.Flags().BoolVar(&recoverOperation, "recover", false, "Resolve positively observed terminal reconcile tasks with a separate recovery grant")
	cmd.Flags().BoolVar(&herdr, "herdr", false, "Inspect the selected accepted Herdr component")
	cmd.Flags().BoolVar(&gateway, "gateway", false, "Inspect the selected accepted protected gateway component")
	cmd.Flags().StringVar(&workload, "workload", "", "Exact accepted workload ID")
	cmd.Flags().StringVar(&revision, "revision", "", "Exact accepted revision ID, current or historical")
	_ = cmd.MarkFlagRequired("revision")
	state.AddCommand(cmd)
}

func nativeOperationSelection(workload string, herdr, gateway, recoverOperation bool) (target, inspection string, err error) {
	if herdr && gateway || workload != "" && (herdr || gateway) || workload == "" && !herdr && !gateway {
		return "", "", errors.New("select exactly one workload, Herdr or gateway")
	}
	if recoverOperation && (herdr || gateway) {
		return "", "", errors.New("component recovery is not enabled; inspect the operation without replay")
	}
	switch {
	case herdr:
		return "herdr-client", "herdr-inspect", nil
	case gateway:
		return "gateway-credentials", "gateway-inspect", nil
	default:
		return workload, "inspect", nil
	}
}
