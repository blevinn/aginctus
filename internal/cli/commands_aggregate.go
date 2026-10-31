package cli

import (
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

// addAggregateCommands makes syntax checking independent of configuration
// loading and of the aggregate orchestration implementation.
func addAggregateCommands(
	root *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int,
) {
	apply := &cobra.Command{
		Use: "apply", Short: "Apply the complete configured orchestration",
		Args: cobra.NoArgs,
	}
	var render bool
	var from string
	apply.Flags().BoolVar(&render, "render", false, "Render the plan without mutation")
	apply.Flags().StringVar(&from, "from", "", "Read a YAML/JSON plan stream from a regular file")
	apply.PreRunE = func(cmd *cobra.Command, _ []string) error {
		if render && cmd.Flags().Changed("from") {
			return fmt.Errorf("--render and --from cannot be used together")
		}
		return nil
	}
	apply.RunE = func(cmd *cobra.Command, _ []string) error {
		// Explicit empty --from was an operation error in v0.1, not the
		// default generated plan. Keep that behavior after typed parsing.
		if cmd.Flags().Changed("from") && from == "" {
			fmt.Fprintln(stderr, "load orchestration: --from requires a regular file path")
			*status = 1
			return nil
		}
		effective, err := load()
		if err == nil {
			*status = executeAggregateApply(cmd.Context(), render, from, stdout, stderr, effective)
		}
		return nil
	}
	root.AddCommand(apply)
	for _, command := range []struct {
		name string
		run  func(*cobra.Command, []string, io.Writer, io.Writer, *config.Config) int
	}{
		{"remove", func(cmd *cobra.Command, _ []string, out, errors io.Writer, cfg *config.Config) int {
			return runAggregateRemove(cmd.Context(), nil, out, errors, cfg)
		}},
		{"status", func(cmd *cobra.Command, _ []string, out, errors io.Writer, cfg *config.Config) int {
			return runAggregateStatus(cmd.Context(), nil, out, errors, cfg)
		}},
	} {
		runner := command.run
		root.AddCommand(&cobra.Command{
			Use: command.name, Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				effective, err := load()
				if err == nil {
					*status = runner(cmd, args, stdout, stderr, effective)
				}
				return nil
			},
		})
	}
}
