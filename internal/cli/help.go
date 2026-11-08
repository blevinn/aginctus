package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

// writeCobraHelp renders syntax directly from the command/flag tree. No
// separate command vocabulary or hand-maintained usage strings exist here.
// Help never loads configuration, touches private registries or queries Incus.
func writeCobraHelp(cmd *cobra.Command, out io.Writer) {
	if cmd.Long != "" {
		fmt.Fprintln(out, cmd.Long)
		fmt.Fprintln(out)
	}
	fmt.Fprint(out, cmd.UsageString())
	if cmd == cmd.Root() || !cmd.HasAvailableSubCommands() {
		return
	}

	// Family help lists descendants so nested, guarded actions remain
	// discoverable without a second manually maintained command inventory.
	fmt.Fprintln(out, "\nSubcommand syntax (derived from registered Cobra commands):")
	var visit func(*cobra.Command)
	visit = func(parent *cobra.Command) {
		for _, child := range parent.Commands() {
			if child.Hidden || !child.IsAvailableCommand() || child.Name() == "help" {
				continue
			}
			if child.Runnable() {
				args := strings.TrimSpace(strings.TrimPrefix(child.Use, child.Name()))
				line := child.CommandPath()
				if args != "" {
					line += " " + args
				}
				fmt.Fprintf(out, "  %s", line)
				if len(child.Aliases) > 0 {
					fmt.Fprintf(out, "  (aliases: %s)", strings.Join(child.Aliases, ", "))
				}
				if child.Short != "" {
					fmt.Fprintf(out, " — %s", child.Short)
				}
				fmt.Fprintln(out)
				if usage := strings.TrimSpace(child.LocalFlags().FlagUsages()); usage != "" {
					for _, flagLine := range strings.Split(usage, "\n") {
						if strings.TrimSpace(flagLine) != "" {
							fmt.Fprintf(out, "      %s\n", strings.TrimSpace(flagLine))
						}
					}
				}
			}
			visit(child)
		}
	}
	visit(cmd)
}
