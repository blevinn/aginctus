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
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/pkg/client"
	"github.com/spf13/cobra"
)

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
	cmd := &cobra.Command{
		Use: "plan-native", Short: "Preview native dependencies and capabilities from accepted state",
		Long: "Preview an intent-only graph for explicitly selected accepted resources. Gateway selection ends at stopped provisioning. Does not inspect live resources, pin a writer revision, or execute changes.",
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
