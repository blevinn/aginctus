package cli

import (
	"io"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

// Herdr is a nested command family, with its empty invocation reserved for
// the already-existing interactive console (not Bubble Tea).
func addHerdrCommands(
	root *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int,
) {
	herdr := &cobra.Command{Use: "herdr", Short: "Manage or open the Herdr client", Args: cobra.NoArgs}
	herdr.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg, err := load()
		if err == nil {
			*status = runHerdrConsole(cmd.Context(), nil, stdout, stderr, cfg)
		}
		return nil
	}
	root.AddCommand(herdr)
	add := func(parent *cobra.Command, name string, aliases []string,
		validateArgs cobra.PositionalArgs,
		run func(*cobra.Command, []string, *config.Config) int,
	) *cobra.Command {
		command := &cobra.Command{Use: name, Aliases: aliases, Args: validateArgs}
		command.RunE = func(cmd *cobra.Command, args []string) error {
			cfg, err := load()
			if err == nil {
				*status = run(cmd, args, cfg)
			}
			return nil
		}
		parent.AddCommand(command)
		return command
	}
	add(herdr, "authorize WORKLOAD", nil, cobra.ExactArgs(1), func(cmd *cobra.Command, args []string, cfg *config.Config) int {
		return runHerdrAuthorize(cmd.Context(), args, stdout, stderr, cfg)
	})
	add(herdr, "pin-host WORKLOAD", nil, cobra.ExactArgs(1), func(cmd *cobra.Command, args []string, cfg *config.Config) int {
		return runHerdrPinHost(cmd.Context(), args, stdout, stderr, cfg)
	})
	add(herdr, "register WORKLOAD", nil, cobra.ExactArgs(1), func(cmd *cobra.Command, args []string, cfg *config.Config) int {
		return runHerdrRegister(cmd.Context(), args, stdout, stderr, cfg)
	})

	client := &cobra.Command{Use: "client", Short: "Manage the Herdr client instance"}
	herdr.AddCommand(client)
	for _, action := range []struct {
		name, operation string
		aliases []string
	}{
		{"apply", "upsert", []string{"ensure"}},
		{"remove", "delete", []string{"teardown"}},
	} {
		operation := action.operation
		var dryRun, force bool
		cmd := add(client, action.name, action.aliases, cobra.NoArgs,
			func(cmd *cobra.Command, _ []string, cfg *config.Config) int {
				return executeHerdrClientOperation(cmd.Context(), operation,
					mutationOptions{DryRun: dryRun, Force: force}, stdout, stderr, cfg)
			})
		cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Inspect without mutation")
		cmd.Flags().BoolVar(&force, "force", false, "Permit supported replacement")
	}
	for _, action := range []string{"start", "stop"} {
		state := action
		add(client, state, nil, cobra.NoArgs, func(cmd *cobra.Command, _ []string, cfg *config.Config) int {
			return transitionHerdrClientCommand(cmd.Context(), state, stdout, stderr, cfg)
		})
	}
	var project string
	stat := add(client, "status", nil, cobra.NoArgs, func(cmd *cobra.Command, _ []string, cfg *config.Config) int {
		if cmd.Flags().Changed("project") {
			return runScopedHerdrClientStatus(cmd.Context(), []string{"--project", project}, stdout, stderr, cfg)
		}
		return runHerdrClientStatus(cmd.Context(), nil, stdout, stderr, cfg)
	})
	stat.Flags().StringVar(&project, "project", "", "Read status in an explicitly owned local project")
}
