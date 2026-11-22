package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	clioutput "github.com/blevinn/aginctus/internal/cli/output"
	"github.com/blevinn/aginctus/pkg/client"
	"github.com/spf13/cobra"
)

// registerAcceptancePreview adds a read-only advisory view over the same
// verified configured-vs-accepted diff capability used by state diff.
// It never commits a candidate, resolves privileges or executes a workload.
func registerAcceptancePreview(
	state *cobra.Command, out io.Writer, service *client.StateService,
	format func() (clioutput.Format, error), project, environmentID *string,
) {
	state.AddCommand(&cobra.Command{
		Use: "plan-accept",
		Short: "Preview accepted-state changes without promoting or deleting resources",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f, err := format()
			if err != nil { return err }
			plan, err := service.PlanAcceptance(cmd.Context(), client.PlanAcceptanceRequest{
				Project:*project, EnvironmentID:*environmentID,
			})
			if err != nil { return err }
			switch f {
			case clioutput.JSON, clioutput.YAML:
				return clioutput.WriteStructured(out, f, plan)
			case clioutput.Name:
				for _, change := range plan.Changes {
					if _, err := fmt.Fprintln(out, change.Resource); err != nil { return err }
				}
				return nil
			case clioutput.Table, clioutput.Wide:
				writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
				if _, err := fmt.Fprintln(writer, "RESOURCE\tADVISORY ACTION"); err != nil { return err }
				for _, change := range plan.Changes {
					if _, err := fmt.Fprintf(writer, "%s\t%s\n", change.Resource, change.Action); err != nil { return err }
				}
				return writer.Flush()
			default:
				return fmt.Errorf("unsupported acceptance preview format %q", f)
			}
		},
	})
}
