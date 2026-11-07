package cli

import (
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

// Named-project image commands never inherit the v0.1 ambient-project
// fallback. The existing image execution routines retain the ownership,
// no-adoption, writer-lease and create-only safety checks.
func addImageCommands(root *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int,
) {
	images := &cobra.Command{Use: "images", Short: "Inspect and manage local Incus images"}
	root.AddCommand(images)
	add := func(name string, aliases []string, rule cobra.PositionalArgs,
		validate func(*cobra.Command, []string) error,
		run func(*cobra.Command, []string, *config.Config) int,
	) *cobra.Command {
		cmd := &cobra.Command{Use: name, Aliases: aliases, Args: rule, PreRunE: validate}
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			cfg, err := load()
			if err == nil {
				*status = run(cmd, args, cfg)
			}
			return nil
		}
		images.AddCommand(cmd)
		return cmd
	}
	var listProject string
	list := add("list", []string{"ls"}, cobra.NoArgs, nil, func(cmd *cobra.Command, _ []string, cfg *config.Config) int {
		if !cmd.Flags().Changed("project") {
			return runImages(cmd.Context(), []string{"list"}, stdout, stderr)
		}
		return runConfiguredImages(cmd.Context(), []string{"list", "--project", listProject}, stdout, stderr, cfg)
	})
	list.Flags().StringVar(&listProject, "project", "", "Explicit owned local project (read-only)")
	add("update NAME PATH", nil, cobra.ExactArgs(2), nil, func(cmd *cobra.Command, args []string, _ *config.Config) int {
		return runImages(cmd.Context(), []string{"update", args[0], args[1]}, stdout, stderr)
	})
	for _, action := range []string{"import", "import-owned", "owner-audit", "replace-plan"} {
		verb := action
		var project, owner string
		var confirm bool
		requireOwner := verb != "import"
		requireCreate := verb == "import" || verb == "import-owned"
		argCount := 2
		if verb == "owner-audit" {
			argCount = 1
		}
		validate := func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("project") {
				return fmt.Errorf("%s requires an explicit --project", verb)
			}
			if requireOwner && (!cmd.Flags().Changed("owner") || (owner != "system" && owner != "user")) {
				return fmt.Errorf("%s requires --owner system|user", verb)
			}
			if requireCreate && !confirm {
				return fmt.Errorf("%s requires affirmative --confirm-create", verb)
			}
			return nil
		}
		command := add(verb+" ALIAS [PATH]", nil, cobra.ExactArgs(argCount), validate,
			func(cmd *cobra.Command, args []string, cfg *config.Config) int {
				legacy := []string{verb, "--project", project}
				if requireOwner {
					legacy = append(legacy, "--owner", owner)
				}
				legacy = append(legacy, args...)
				if requireCreate {
					legacy = append(legacy, "--confirm-create")
				}
				return runConfiguredImages(cmd.Context(), legacy, stdout, stderr, cfg)
			})
		command.Flags().StringVar(&project, "project", "", "Explicit local Aginctus project")
		if requireOwner {
			command.Flags().StringVar(&owner, "owner", "", "Claimed lifecycle authority: system or user")
		}
		if requireCreate {
			command.Flags().BoolVar(&confirm, "confirm-create", false, "Confirm create-only image import")
		}
	}
}
