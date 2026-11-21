package clihelp

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

// WriteCobraHelp renders compact navigational help from the authoritative
// Cobra tree. It deliberately lists only immediate children; nested syntax
// and inherited flags belong in explicit reference help.
func WriteCobraHelp(cmd *cobra.Command, out io.Writer) {
	if err := RenderBasic(cmd, out); err != nil {
		// Fail loudly rather than silently truncating the operator's command map.
		fmt.Fprintf(out, "Help layout error: %v\n", err)
	}
}

// WriteCobraReference preserves the original, exhaustive Cobra-derived
// syntax inventory for documentation validation and explicit reference use.
// It must never be invoked by the default/basic help renderer.
func WriteCobraReference(cmd *cobra.Command, out io.Writer) {
	if cmd.Long != "" {
		fmt.Fprintln(out, cmd.Long)
		fmt.Fprintln(out)
	}
	fmt.Fprint(out, cmd.UsageString())
	if !cmd.HasAvailableSubCommands() {
		return
	}

	fmt.Fprintln(out, "\nSubcommand syntax (derived from registered Cobra commands):")
	var visit func(*cobra.Command)
	visit = func(parent *cobra.Command) {
		for _, child := range parent.Commands() {
			if child.Hidden || (!child.IsAvailableCommand() && child.Name() != "help") {
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
