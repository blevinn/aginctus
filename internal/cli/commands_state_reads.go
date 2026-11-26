package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	clioutput "github.com/blevinn/aginctus/internal/cli/output"
	"github.com/blevinn/aginctus/pkg/api"
	"github.com/blevinn/aginctus/pkg/client"
	"github.com/spf13/cobra"
)

// registerStateReads supports read-only state inspection only. No selected
// project/UUID is ever treated as authentication evidence, and no state
// service is implicitly instantiated by the CLI.
func registerStateReads(root *cobra.Command, out io.Writer, configured client.ConfiguredStateReader, accepted client.AcceptedStateReader, diff client.ConfiguredAcceptedDiffReader, project, environmentID, requestedFormat *string) {
	state := &cobra.Command{Use: "state", Short: "Inspect configured and accepted environment state", Args: cobra.NoArgs}
	root.AddCommand(state)
	service := client.New(client.Options{
		ConfiguredStateReader: configured, AcceptedStateReader: accepted,
		ConfiguredAcceptedDiffReader: diff,
	}).State
	format := func() (clioutput.Format, error) {
		if !root.PersistentFlags().Changed("output") { return clioutput.Table, nil }
		return clioutput.ParseFormat(*requestedFormat)
	}
	state.AddCommand(&cobra.Command{
		Use: "show", Short: "Inspect one state layer without promoting it", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f, err := format()
			if err != nil { return err }
			layer, err := cmd.Flags().GetString("layer")
			if err != nil { return err }
			switch layer {
			case "configured":
				result, err := service.Configured(cmd.Context(), client.GetConfiguredStateRequest{Project: *project, EnvironmentID: *environmentID})
				if err != nil { return err }
				if f == clioutput.JSON || f == clioutput.YAML { return clioutput.WriteStructured(out, f, result) }
				if f == clioutput.Name {
					for _, item := range result.Items { if _, err := fmt.Fprintln(out, item.ID); err != nil { return err } }
					return nil
				}
				return printStateTable(out, f, "WORKLOAD", "INSTANCE", func(w io.Writer) error {
					for _, item := range result.Items { if _, err := fmt.Fprintf(w, "%s\t%s\n", item.ID, item.InstanceName); err != nil { return err } }
					return nil
				})
			case "accepted":
				result, err := service.Accepted(cmd.Context(), client.ReadStateRequest{Project: *project, EnvironmentID: *environmentID})
				if err != nil { return err }
				return renderAcceptedState(out, f, result)
			default:
				return fmt.Errorf("unsupported state layer %q (supported: configured, accepted)", layer)
			}
		},
	})
	show := state.Commands()[0]
	show.Flags().String("layer", "accepted", "State layer: configured or accepted (explicit provenance)")
	registerAcceptancePreview(state, out, service, format, project, environmentID)
	registerNativeIntentPlan(state, out, format, project, environmentID, readInstalledAcceptedSnapshot)
	registerNativeWorkloadReconcile(state, out, format, project, environmentID)
	registerNativeOperationInspection(state, out, format, project, environmentID)
	registerStatePromotion(state, out, format, project, environmentID)
	state.AddCommand(&cobra.Command{
		Use: "diff", Short: "Compare accepted state to configured candidate (read only)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f, err := format()
			if err != nil { return err }
			result, err := service.Diff(cmd.Context(), client.ReadStateRequest{Project: *project, EnvironmentID: *environmentID})
			if err != nil { return err }
			if f == clioutput.JSON || f == clioutput.YAML { return clioutput.WriteStructured(out, f, result) }
			if f == clioutput.Name {
				for _, id := range result.NewlySelected { if _, err := fmt.Fprintln(out, id); err != nil { return err } }
				for _, id := range result.Unselected { if _, err := fmt.Fprintln(out, id); err != nil { return err } }
				for _, item := range result.Modified { if _, err := fmt.Fprintln(out, item.ID); err != nil { return err } }
				return nil
			}
			return printStateTable(out, f, "FIELD", "VALUE", func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "FROM\t%s\nTO\t%s\nCHANGED\t%t\nNEWLY SELECTED\t%d\nUNSELECTED\t%d\nMODIFIED\t%d\n",
					result.FromLayer, result.ToLayer, result.Changed, len(result.NewlySelected), len(result.Unselected), len(result.Modified))
				return err
			})
		},
	})
}
func printStateTable(out io.Writer, format clioutput.Format, heading1, heading2 string, render func(io.Writer) error) error {
	if format != clioutput.Table && format != clioutput.Wide { return fmt.Errorf("unsupported state output format %q", format) }
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintf(w, "%s\t%s\n", heading1, heading2); err != nil { return err }
	if err := render(w); err != nil { return err }
	return w.Flush()
}
func renderAcceptedState(out io.Writer, format clioutput.Format, result api.AcceptedStateStatus) error {
	if format == clioutput.JSON || format == clioutput.YAML { return clioutput.WriteStructured(out, format, result) }
	if format == clioutput.Name {
		if result.Revision == nil { return nil }
		_, err := fmt.Fprintln(out, result.Revision.ID)
		return err
	}
	return printStateTable(out, format, "FIELD", "VALUE", func(w io.Writer) error {
		if _, err := fmt.Fprintf(w, "ENVIRONMENT\t%s\nLAYER\taccepted\nHAS ACCEPTED\t%t\n",
			result.Environment.ID, result.HasAccepted); err != nil { return err }
		if result.Revision != nil {
			_, err := fmt.Fprintf(w, "REVISION\t%s\nGENERATION\t%d\n", result.Revision.ID, result.Revision.Generation)
			return err
		}
		return nil
	})
}
