package registrycmd

import (
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

type Handlers struct {
	CheckMutation func(*config.Config, string) error
	Register func(string, string, string, string, string, bool, io.Writer, io.Writer) int
	RenderModel func(*config.Config, string, string) ([]byte, error)
	AddAssets func(*cobra.Command, *cobra.Command, *cobra.Command, *int, config.Options, func() (*config.Config, error))
}

// Register owns the provider/model Cobra declaration and legacy-compatible
// register/render syntax. Underlying store mutations stay in cli services.
func Register(
	gateway *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int, options config.Options, handlers Handlers,
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
	var providerAlias string
	var providerDryRun bool
	providerRegister := add(providers, "register TYPE", cobra.ExactArgs(1),
		func(cmd *cobra.Command, args []string, cfg *config.Config) int {
			name := providerAlias
			if name == "" {
				name = args[0]
			}
			if !providerDryRun {
				if err := handlers.CheckMutation(cfg, name); err != nil {
					fmt.Fprintln(stderr, err)
					return 1
				}
			}
			return handlers.Register("provider", args[0], "", "", providerAlias, providerDryRun, stdout, stderr)
		})
	providerRegister.Flags().StringVar(&providerAlias, "alias", "", "Local provider alias")
	providerRegister.Flags().BoolVar(&providerDryRun, "dry-run", false, "Validate without registry mutation")

	models := &cobra.Command{Use: "model", Short: "Inspect and register model aliases"}
	gateway.AddCommand(models)
	var renderProvider string
	render := add(models, "render NAME", cobra.ExactArgs(1), func(_ *cobra.Command, args []string, cfg *config.Config) int {
		data, err := handlers.RenderModel(cfg, renderProvider, args[0])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, string(data))
		fmt.Fprintln(stderr, "model definition rendered for review only; gateway routing and credentials remain inactive")
		return 0
	})
	render.Flags().StringVar(&renderProvider, "provider", "", "Provider instance offering the model")
	var providerAliasName, source string
	var modelDryRun bool
	register := add(models, "register NAME", cobra.ExactArgs(1), func(cmd *cobra.Command, args []string, cfg *config.Config) int {
		name := providerAliasName
		if name == "" {
			name = "chatgpt"
		}
		if !modelDryRun {
			if err := handlers.CheckMutation(cfg, name); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
		}
		return handlers.Register("model", args[0], providerAliasName, source, "", modelDryRun, stdout, stderr)
	})
	register.Flags().StringVar(&providerAliasName, "provider", "", "Registered provider alias")
	register.Flags().StringVar(&source, "source", "", "Responses upstream model source")
	register.Flags().BoolVar(&modelDryRun, "dry-run", false, "Validate without registry mutation")
	handlers.AddAssets(gateway, providers, models, status, options, load)
}
