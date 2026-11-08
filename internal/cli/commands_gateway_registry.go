package cli

import (
	"io"

	registrycmd "github.com/blevinn/aginctus/internal/cli/commands/gateway/registry"
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
	load func() (*config.Config, error), status *int, options config.Options,
) {
	registrycmd.Register(gateway, stdout, stderr, load, status, options, registrycmd.Handlers{
		CheckMutation: checkGatewayProviderMutation,
		Register: executeGatewayRegistration,
		RenderModel: func(cfg *config.Config, provider, name string) ([]byte, error) {
			store, err := configuredGatewayStore(cfg)
			if err != nil {
				return nil, err
			}
			return renderProviderModel(store, provider, name)
		},
		AddAssets: addGatewayAssetCommands,
	})
}
