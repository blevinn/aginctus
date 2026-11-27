package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	clioutput "github.com/blevinn/aginctus/internal/cli/output"
	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/nativelifecycle"
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/pkg/api"
	"github.com/blevinn/aginctus/pkg/client"
	"github.com/spf13/cobra"
)

type observedWorkloadPlan struct {
	api.TypeMeta `json:",inline"`
	Revision     string               `json:"revision"`
	WriterPin    bool                 `json:"writerPin"`
	Result       orchestration.Result `json:"result"`
}

func readInstalledAcceptedSnapshot(ctx context.Context, project, id string) (environment.AcceptedRevision, environment.Candidate, error) {
	path := os.Getenv("AGINCTUS_RESTRICTED_READ_CONFIG")
	if path == "" {
		return environment.AcceptedRevision{}, environment.Candidate{}, errors.New("state plan-native requires AGINCTUS_RESTRICTED_READ_CONFIG and an authenticated project-state service")
	}
	readers, closeReads, err := environment.OpenProductionOperatorReads(ctx, path)
	if err != nil {
		return environment.AcceptedRevision{}, environment.Candidate{}, err
	}
	defer closeReads()
	if readers.AcceptedSnapshot == nil {
		return environment.AcceptedRevision{}, environment.Candidate{}, errors.New("authenticated project-state service configuration required")
	}
	return readers.AcceptedSnapshot.ReadAcceptedSnapshot(ctx, project, id)
}

func registerNativeIntentPlan(state *cobra.Command, out io.Writer, format func() (clioutput.Format, error), project, id *string,
	read func(context.Context, string, string) (environment.AcceptedRevision, environment.Candidate, error),
) {
	var selection orchestration.AcceptedIntentSelection
	var hostGrant, expected string
	var bootstrapHerdr bool
	cmd := &cobra.Command{
		Use: "plan-native", Short: "Preview native dependencies and capabilities from accepted state",
		Long: "Preview an intent-only graph for explicitly selected accepted resources. For one workload, --host-grant and --expect enable a point-in-time live preview. Neither mode pins a writer revision or executes changes.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f, err := format()
			if err != nil {
				return err
			}
			if f != clioutput.Table && f != clioutput.JSON && f != clioutput.YAML {
				return errors.New("state plan-native supports table, json or yaml output")
			}
			if _, err := incusscope.NewProjectScope(*project); err != nil || !client.IsEnvironmentUUID(*id) {
				return errors.New("state plan-native requires an explicit nondefault Incus project and immutable environment UUID")
			}
			if err := selection.Validate(); err != nil {
				return err
			}
			if cmd.Flags().Changed("host-grant") || cmd.Flags().Changed("expect") || bootstrapHerdr {
				if selection.Herdr && !selection.All && !selection.Gateway && len(selection.Workloads)==0 && hostGrant!="" && expected!="" && f==clioutput.JSON {
					result,_,err:=nativelifecycle.ExecuteInstalledHerdr(cmd.Context(),os.Getenv("AGINCTUS_RESTRICTED_READ_CONFIG"),hostGrant,*project,*id,expected,bootstrapHerdr,true)
					if err!=nil{return err}
					return clioutput.WriteStructured(out,f,observedWorkloadPlan{TypeMeta:api.TypeMeta{APIVersion:"aginctus.dev/v1alpha1",Kind:"ObservedNativeHerdrPlan"},Revision:expected,Result:result})
				}
				if bootstrapHerdr{return errors.New("Herdr bootstrap preview requires explicit Herdr selection and live grant")}
				if hostGrant == "" || expected == "" || len(selection.Workloads) != 1 || selection.All || selection.Herdr || selection.Gateway || f != clioutput.JSON {
					return errors.New("live native planning requires one workload, --host-grant, --expect, and json output")
				}
				result, err := nativelifecycle.PlanInstalledWorkload(cmd.Context(), os.Getenv("AGINCTUS_RESTRICTED_READ_CONFIG"), hostGrant, *project, *id, expected, selection.Workloads[0])
				if err != nil {
					return err
				}
				return clioutput.WriteStructured(out, f, observedWorkloadPlan{
					TypeMeta: api.TypeMeta{APIVersion: "aginctus.dev/v1alpha1", Kind: "ObservedNativeWorkloadPlan"}, Revision: expected, Result: result,
				})
			}
			rev, accepted, err := read(cmd.Context(), *project, *id)
			if err != nil {
				return err
			}
			if rev.Project != *project || rev.EnvironmentID != *id {
				return errors.New("accepted snapshot outside requested scope")
			}
			preview, err := orchestration.PreviewAcceptedIntent(rev, accepted, selection)
			if err != nil {
				return err
			}
			if err := cmd.Context().Err(); err != nil {
				return err
			}
			return renderNativeIntentPlan(out, f, preview)
		},
	}
	cmd.Flags().StringArrayVar(&selection.Workloads, "workload", nil, "Accepted native workload ID (repeatable)")
	cmd.Flags().BoolVar(&selection.Herdr, "herdr", false, "Select accepted Herdr intent")
	cmd.Flags().BoolVar(&selection.Gateway, "gateway", false, "Select accepted gateway stopped provisioning")
	cmd.Flags().BoolVar(&selection.All, "all", false, "Select the full accepted scope (cannot be combined with other selectors)")
	cmd.Flags().BoolVar(&bootstrapHerdr,"bootstrap-herdr-state",false,"Preview separately authorized initial Herdr state creation")
	cmd.Flags().StringVar(&hostGrant, "host-grant", "", "Administrator-owned attachment grant for single-workload live preview")
	cmd.Flags().StringVar(&expected, "expect", "", "Exact accepted revision for single-workload live preview")
	state.AddCommand(cmd)
}

func renderNativeIntentPlan(out io.Writer, format clioutput.Format, p orchestration.AcceptedIntentPreview) error {
	if format != clioutput.Table {
		return clioutput.WriteStructured(out, format, p)
	}
	if _, err := fmt.Fprintf(out, "Accepted snapshot: %s (%s/%s)\nMode: %s; live state: %s; writer pin: %t\n",
		p.Selection.Revision.ID, p.Selection.Revision.Project, p.Selection.Revision.EnvironmentID, p.Mode, p.LiveState, p.WriterPin); err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "STAGE\tKIND\tDEPENDS ON"); err != nil {
		return err
	}
	for _, r := range p.Selection.Resources {
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\n", r.ID, r.Kind, strings.Join(r.DependsOn, ", ")); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "\nCAPABILITY\tSTATUS"); err != nil {
		return err
	}
	for _, capability := range p.Capabilities {
		if _, err := fmt.Fprintf(w, "%s\t%s\n", capability.ID, capability.Status); err != nil {
			return err
		}
	}
	return w.Flush()
}
