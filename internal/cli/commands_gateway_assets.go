package cli

import (
	"github.com/blevinn/aginctus/internal/config"
	assetscmd "github.com/blevinn/aginctus/internal/cli/commands/gateway/assets"
	"github.com/blevinn/aginctus/internal/gatewaycontrol"
	siwc "github.com/blevinn/aginctus/internal/plugins/openai-siwc"
	"github.com/spf13/cobra"
)

// Other in-package gateway operations use this compiled registry as well.
func gatewayPlugins() gatewaycontrol.Plugins {
	plugins := gatewaycontrol.Builtins()
	plugins["openai-siwc"] = siwc.Plugin()
	return plugins
}

func addGatewayAssetCommands(gateway, providers, models *cobra.Command,
	status *int, options config.Options, load func() (*config.Config, error),
) {
	assetscmd.Register(gateway, providers, models, status, options, load, configuredGatewayStore, gatewayPlugins())
}
