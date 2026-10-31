package cli

import (
	"io"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

// Positional-first v0.1 forms are translated to their unambiguous verb-first
// equivalents before parsing. A resource literally named "register" or "show"
// can always be addressed using the canonical explicit action-first form.
func normalizeGatewayRegistryLegacyArgs(args []string) []string {
	if len(args) < 4 || args[0] != "gateway" {
		return args
	}
	group := args[1]
	if group != "provider" && group != "model" {
		return args
	}
	var action string
	switch args[3] {
	case "register", "show":
		action = args[3]
	case "render":
		if group != "model" {
			return args
		}
		action = "render"
	default:
		return args
	}
	// Prefer declared commands when the first token is a verb; this
	// resolves syntactic ambiguity without silently rewriting new syntax.
	switch args[2] {
	case "list", "show", "register", "render":
		return args
	}
	mapped := append([]string{"gateway", group, action, args[2]}, args[4:]...)
	return mapped
}

func addGatewayRegistryCommands(
	gateway *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int,
) {
	add := func(parent *cobra.Command, name string, argsRule cobra.PositionalArgs,
		run func(*cobra.Command, []string, *config.Config) int,
	) *cobra.Command {
		cmd := &cobra.Command{
			Use: name, Args: argsRule,
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, err := load()
				if err == nil {
					*status = run(cmd, args, cfg)
				}
				return nil
			},
		}
		parent.AddCommand(cmd)
		return cmd
	}
	providers := &cobra.Command{Use: "provider", Short: "Inspect and register model provider aliases"}
	gateway.AddCommand(providers)
	add(providers, "list", cobra.NoArgs, func(cmd *cobra.Command, _ []string, cfg *config.Config) int {
		return runGateway(cmd.Context(), []string{"provider", "list"}, stdout, stderr, nil, cfg)
	})
	add(providers, "show ALIAS", cobra.ExactArgs(1), func(cmd *cobra.Command, args []string, cfg *config.Config) int {
		return runGateway(cmd.Context(), []string{"provider", args[0], "show"}, stdout, stderr, nil, cfg)
	})
	var providerAlias string
	var providerDryRun bool
	providerRegister := add(providers, "register TYPE", cobra.ExactArgs(1),
		func(cmd *cobra.Command, args []string, cfg *config.Config) int {
			legacy := []string{"provider", args[0], "register"}
			if cmd.Flags().Changed("alias") {
				legacy = append(legacy, "--alias="+providerAlias)
			}
			if providerDryRun {
				legacy = append(legacy, "--dry-run")
			}
			return runGateway(cmd.Context(), legacy, stdout, stderr, nil, cfg)
		})
	providerRegister.Flags().StringVar(&providerAlias, "alias", "", "Local provider alias")
	providerRegister.Flags().BoolVar(&providerDryRun, "dry-run", false, "Validate without registry mutation")

	models := &cobra.Command{Use: "model", Short: "Inspect and register model aliases"}
	gateway.AddCommand(models)
	add(models, "list", cobra.NoArgs, func(cmd *cobra.Command, _ []string, cfg *config.Config) int {
		return runGateway(cmd.Context(), []string{"model", "list"}, stdout, stderr, nil, cfg)
	})
	add(models, "show NAME", cobra.ExactArgs(1), func(cmd *cobra.Command, args []string, cfg *config.Config) int {
		return runGateway(cmd.Context(), []string{"model", args[0], "show"}, stdout, stderr, nil, cfg)
	})
	add(models, "render NAME", cobra.ExactArgs(1), func(cmd *cobra.Command, args []string, cfg *config.Config) int {
		return runGateway(cmd.Context(), []string{"model", args[0], "render"}, stdout, stderr, nil, cfg)
	})
	var providerAliasName, source string
	var modelDryRun bool
	register := add(models, "register NAME", cobra.ExactArgs(1), func(cmd *cobra.Command, args []string, cfg *config.Config) int {
		legacy := []string{"model", args[0], "register"}
		if cmd.Flags().Changed("provider") {
			legacy = append(legacy, "--provider="+providerAliasName)
		}
		if cmd.Flags().Changed("source") {
			legacy = append(legacy, "--source="+source)
		}
		if modelDryRun {
			legacy = append(legacy, "--dry-run")
		}
		return runGateway(cmd.Context(), legacy, stdout, stderr, nil, cfg)
	})
	register.Flags().StringVar(&providerAliasName, "provider", "", "Registered provider alias")
	register.Flags().StringVar(&source, "source", "", "Responses upstream model source")
	register.Flags().BoolVar(&modelDryRun, "dry-run", false, "Validate without registry mutation")
}
