package operatorcmd

import (
	"context"
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

// Operator diagnostics deliberately never use ambient or implicit project
// selection. Offline and host-only operations reject configuration overrides.
type Handlers struct {
	UnitsAudit func(context.Context, []string, io.Writer, io.Writer) int
	PackageAudit func([]string, io.Writer, io.Writer) int
	StateAudit func([]string, io.Writer, io.Writer, *config.Config) int
	Preflight func(context.Context, []string, io.Writer, io.Writer) int
	ProjectEnsure func(context.Context, []string, io.Writer, io.Writer) int
	ProjectPlan func(context.Context, []string, io.Writer, io.Writer) int
}

// Register keeps owner/project checks in the Cobra leaves, before any
// config loading or operator-side side effect.
func Register(root *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int, options *config.Options, handlers Handlers,
) {
	operator := &cobra.Command{Use: "operator", Short: "Inspect operator and project authority"}
	root.AddCommand(operator)
	add := func(name string, withConfig bool, owner, project *string, confirmed *bool,
		run func(*cobra.Command, *config.Config) int,
	) *cobra.Command {
		cmd := &cobra.Command{Use: name, Args: cobra.NoArgs}
		cmd.PreRunE = func(cmd *cobra.Command, _ []string) error {
			if !withConfig && (options.ConfigurationFile != "" || len(options.Overrides) != 0) {
				return fmt.Errorf("%s: configuration overrides are not supported", cmd.CommandPath())
			}
			if owner != nil && *owner != "system" && *owner != "user" {
				return fmt.Errorf("--owner must be system or user")
			}
			if project != nil && !cmd.Flags().Changed("project") {
				return fmt.Errorf("explicit --project NAME is required")
			}
			if confirmed != nil && !*confirmed {
				return fmt.Errorf("explicit --confirm-create required")
			}
			return nil
		}
		cmd.RunE = func(cmd *cobra.Command, _ []string) error {
			var cfg *config.Config
			if withConfig {
				var err error
				cfg, err = load()
				if err != nil {
					return nil
				}
			}
			*status = run(cmd, cfg)
			return nil
		}
		operator.AddCommand(cmd)
		return cmd
	}
	for _, verb := range []string{"units-audit", "package-audit", "state-audit"} {
		action := verb
		owner := ""
		cfg := action == "state-audit"
		cmd := add(action, cfg, &owner, nil, nil, func(cmd *cobra.Command, config *config.Config) int {
			if !cmd.Flags().Changed("owner") {
				return 2
			}
			args := []string{"--owner", owner}
			switch action {
			case "units-audit":
				return handlers.UnitsAudit(cmd.Context(), args, stdout, stderr)
			case "package-audit":
				return handlers.PackageAudit(args, stdout, stderr)
			default:
				return handlers.StateAudit(append([]string{"state-audit"}, args...), stdout, stderr, config)
			}
		})
		cmd.Flags().StringVar(&owner, "owner", "", "Declared authority: system or user")
		// Avoid bypassing the existing no-argument owner requirement.
		prior := cmd.PreRunE
		cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("owner") {
				return fmt.Errorf("--owner system|user is required")
			}
			return prior(cmd, args)
		}
	}
	preflight := ""
	pre := add("preflight", true, nil, &preflight, nil, func(cmd *cobra.Command, _ *config.Config) int {
		return handlers.Preflight(cmd.Context(), []string{"preflight", "--project", preflight}, stdout, stderr)
	})
	pre.Flags().StringVar(&preflight, "project", "", "Explicit Incus project")
	for _, action := range []string{"project-plan", "project-ensure"} {
		verb := action
		var project string
		var confirm bool
		var gate *bool
		if action == "project-ensure" {
			gate = &confirm
		}
		cmd := add(verb, false, nil, &project, gate, func(cmd *cobra.Command, _ *config.Config) int {
			args := []string{"--project", project}
			if verb == "project-ensure" {
				return handlers.ProjectEnsure(cmd.Context(), append(args, "--confirm-create"), stdout, stderr)
			}
			return handlers.ProjectPlan(cmd.Context(), args, stdout, stderr)
		})
		cmd.Flags().StringVar(&project, "project", "", "Explicit locally owned project")
		if gate != nil {
			cmd.Flags().BoolVar(&confirm, "confirm-create", false, "Confirm an atomic create-only local project operation")
		}
	}
}
