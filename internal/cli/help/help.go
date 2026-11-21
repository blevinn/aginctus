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
	if cmd.Short != "" {
		fmt.Fprintln(out, cmd.Short)
		fmt.Fprintln(out)
	} else if cmd.Long != "" {
		fmt.Fprintln(out, strings.SplitN(cmd.Long, "\n", 2)[0])
		fmt.Fprintln(out)
	}
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintf(out, "  %s\n", cmd.UseLine())

	children := make([]*cobra.Command, 0)
	for _, child := range cmd.Commands() {
		if child.Hidden || !child.IsAvailableCommand() {
			continue
		}
		children = append(children, child)
	}
	if len(children) > 0 {
		fmt.Fprintln(out, "\nCommands:")
		for _, child := range children {
			// Use the name, not CommandPath: this is navigation, not a
			// flattened syntax reference. Every visible child is listed.
			if child.Short != "" {
				fmt.Fprintf(out, "  %-16s %s\n", child.Name(), child.Short)
			} else {
				fmt.Fprintf(out, "  %s\n", child.Name())
			}
		}
	} else {
		// Avoid inherited/global flag dumps. Local flags are shown when
		// short enough to remain useful; all flags remain in full help.
		usage := strings.TrimRight(cmd.LocalFlags().FlagUsages(), "\n")
		lines := strings.Split(usage, "\n")
		if usage != "" && len(lines) <= 8 {
			fmt.Fprintln(out, "\nOptions:")
			for _, line := range lines {
				fmt.Fprintln(out, line)
			}
		}
	}
	fmt.Fprintln(out)
	if len(children) > 0 {
		fmt.Fprintf(out, "Run '%s <command> --help' for subcommand details.\n", cmd.CommandPath())
	}
	fmt.Fprintf(out, "Run 'aginctus help --full%s' for the complete reference.\n", strings.TrimPrefix(cmd.CommandPath(), "aginctus"))
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
