package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	clioutput "github.com/blevinn/aginctus/internal/cli/output"
	"github.com/blevinn/aginctus/internal/plugins"
	"github.com/blevinn/aginctus/pkg/api"
	"github.com/blevinn/aginctus/pkg/client"
	"github.com/spf13/cobra"
)

// addPluginShowCommand adds a read-only client-backed operation. Compiled
// inventory is process-wide, and neither selection nor output grants Incus
// or runtime plugin capabilities.
func addPluginShowCommand(parent, root *cobra.Command, out io.Writer, registry *plugins.Registry, requestedFormat *string) {
	parent.AddCommand(&cobra.Command{
		Use: "show NAME", Short: "Show one compiled plugin", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format := clioutput.Table
			if root.PersistentFlags().Changed("output") {
				var err error
				format, err = clioutput.ParseFormat(*requestedFormat)
				if err != nil {
					return err
				}
			}
			var inventory client.PluginInventory
			if registry != nil {
				inventory = registry
			}
			detail, err := client.New(client.Options{PluginInventory: inventory}).Plugins.Get(cmd.Context(), client.GetPluginRequest{Name: args[0]})
			if err != nil {
				return err
			}
			return renderPluginDetail(out, format, detail)
		},
	})
}

func renderPluginDetail(out io.Writer, format clioutput.Format, detail api.PluginDetail) error {
	switch format {
	case clioutput.JSON, clioutput.YAML:
		return clioutput.WriteStructured(out, format, detail)
	case clioutput.Name:
		_, err := fmt.Fprintln(out, detail.Name)
		return err
	case clioutput.Table, clioutput.Wide:
		writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintf(writer, "NAME\t%s\nVERSION\t%s\nPLUGIN API\t%s\n", detail.Name, detail.Version, detail.Plugin.APIVersion); err != nil {
			return err
		}
		if format == clioutput.Wide {
			if _, err := fmt.Fprintf(writer, "DEPENDENCIES\t%s\n", strings.Join(detail.Dependencies, ",")); err != nil {
				return err
			}
			for _, cap := range detail.Capabilities {
				if _, err := fmt.Fprintf(writer, "CAPABILITY\t%s (%s)\n", cap.Name, cap.Kind); err != nil {
					return err
				}
			}
		}
		return writer.Flush()
	default:
		return fmt.Errorf("unsupported plugin detail format %q", format)
	}
}
