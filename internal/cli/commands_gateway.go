package cli

import (
	"context"
	"io"
	"os"

	gatewaycmd "github.com/blevinn/aginctus/internal/cli/commands/gateway"
	assetscmd "github.com/blevinn/aginctus/internal/cli/commands/gateway/assets"
	egresscmd "github.com/blevinn/aginctus/internal/cli/commands/gateway/egress"
	registrycmd "github.com/blevinn/aginctus/internal/cli/commands/gateway/registry"
	gatewayops "github.com/blevinn/aginctus/internal/cli/operations/gateway"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gatewaycontrol"
	siwc "github.com/blevinn/aginctus/internal/plugins/openai-siwc"
	"github.com/spf13/cobra"
)

// All gateway command adapters live in one composition file. The registry,
// credential and SIWC capabilities remain explicitly injected and the Cobra
// registration packages still own their syntax and help behavior.

// Credential volumes are manipulated only through the existing injected
// gateway capabilities; the operation package handles result formatting.
func executeGatewayCredential(ctx context.Context, action string, stdout, stderr io.Writer, effective *config.Config) int {
	return gatewayops.ExecuteCredential(ctx, action, stdout, stderr, effective, gatewayops.CredentialActions{
		Audit: auditGatewayCredentials, Attach: attachGatewayCredentials, Ensure: ensureCredentialStorage,
	})
}

func executeGatewayLifecycle(ctx context.Context, action string, force bool, stdout, stderr io.Writer, effective *config.Config) int {
	return gatewayops.ExecuteLifecycle(ctx, action, force, stdout, stderr, effective, gatewayops.LifecycleActions{
		ConfiguredSpec: configuredGatewaySpec,
		Status:         runGatewayStatus,
		SyncSIWC:       syncGatewaySIWC,
	})
}

func addGatewayCoreCommands(
	root *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int,
) *cobra.Command {
	return gatewaycmd.Register(root, stdout, stderr, load, status, gatewaycmd.Handlers{
		Status:     runGatewayStatus,
		Lifecycle:  executeGatewayLifecycle,
		Credential: executeGatewayCredential,
	})
}

// Test-visible input seam preserves offline transcript injection.
var gatewayResponsesInput io.Reader = os.Stdin

func verifyGatewayResponsesStream(args []string, stdout, stderr io.Writer) int {
	return gatewayops.VerifyResponsesStream(args, stdout, stderr, gatewayResponsesInput)
}

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

// Keep an injectable read-only Incus query for network/firewall and gateway tests.
var gatewayEgressQuery = gatewayops.QueryIncusEgress

func executeGatewayEgress(ctx context.Context, action, zone, ranges string,
	stdout, stderr io.Writer, effective *config.Config,
) int {
	return gatewayops.ExecuteEgress(ctx, action, zone, ranges, stdout, stderr, effective, gatewayEgressQuery)
}

func addGatewayEgressCommands(
	gatewayCmd *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int,
) {
	egresscmd.Register(gatewayCmd, stdout, stderr, load, status, egresscmd.Handlers{
		Egress: executeGatewayEgress,
		Verify: verifyGatewayResponsesStream,
	})
}

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
		Register:      executeGatewayRegistration,
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

// Registry actions are bound to the local persistence and rendering capabilities.
// These thin adapters retain the existing CLI test/command contracts.
func executeGatewayModelRead(action, name string, stdout, stderr io.Writer) int {
	return gatewayops.ModelRead(action, name, stdout, stderr, renderRegisteredModel)
}
func executeGatewayProviderRead(action, name string, stdout, stderr io.Writer) int {
	return gatewayops.ProviderRead(action, name, stdout, stderr)
}
func executeGatewayRegistration(kind, name, provider, source, alias string, dryRun bool, stdout, stderr io.Writer) int {
	return gatewayops.Registration(kind, name, provider, source, alias, dryRun, stdout, stderr)
}
