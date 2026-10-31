package cli

import (
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

// Testenv is deliberately isolated from the pilot's config and Incus.
// Parsing a VM command does not start a VM; only the explicit 'vm test'
// leaf can call the established backend.
func addTestenvCommands(root *cobra.Command, stdout, stderr io.Writer,
	status *int, options *config.Options,
) {
	groupRun := func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("%s requires a subcommand", cmd.CommandPath())
		}
		return fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())
	}
	testenv := &cobra.Command{
		Use: "testenv", Short: "Run isolated test environments", RunE: groupRun,
	}
	root.AddCommand(testenv)
	vm := &cobra.Command{
		Use: "vm", Short: "Inspect or exercise a disposable VM scenario", RunE: groupRun,
	}
	testenv.AddCommand(vm)
	add := func(parent *cobra.Command, name string, rule cobra.PositionalArgs, prefix []string) {
		argsFor := append([]string(nil), prefix...)
		cmd := &cobra.Command{Use: name, Args: rule}
		cmd.PreRunE = func(_ *cobra.Command, _ []string) error {
			if options.ConfigurationFile != "" || len(options.Overrides) != 0 {
				return fmt.Errorf("testenv: host configuration options are not supported")
			}
			return nil
		}
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			invoke := append(append([]string(nil), argsFor...), args...)
			*status = runTestenv(cmd.Context(), invoke, stdout, stderr)
			return nil
		}
		parent.AddCommand(cmd)
	}
	add(testenv, "mcp", cobra.NoArgs, []string{"mcp"})
	add(vm, "policy", cobra.NoArgs, []string{"vm", "policy"})
	add(vm, "list", cobra.NoArgs, []string{"vm", "list"})
	add(vm, "backend [NAME]", cobra.MaximumNArgs(1), []string{"vm", "backend"})
	add(vm, "plan SCENARIO", cobra.ExactArgs(1), []string{"vm", "plan"})
	add(vm, "test SCENARIO", cobra.ExactArgs(1), []string{"vm", "test"})
}
