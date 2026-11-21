package assetscmd

import (
	"encoding/json"
	"fmt"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gatewaycontrol"
	"github.com/spf13/cobra"
)

type gatewayAction func(*cobra.Command, []string, gatewaycontrol.AssetOperations) (any, error)

type gatewayAssetBuilder struct {
	plugins gatewaycontrol.Plugins
	status  *int
	options config.Options
	load    func() (*config.Config, error)
	openStore func(*config.Config) (*gatewaycontrol.Store, error)
	readConfig func(string, string, any) error
}

// Register contributes the plugin/provider/model/MCP/catalog subtrees. Store
// opening stays behind an injected trusted host boundary rather than importing cli.
func Register(gateway, providers, models *cobra.Command, status *int, options config.Options,
	load func() (*config.Config, error), openStore func(*config.Config) (*gatewaycontrol.Store, error),
	plugins gatewaycontrol.Plugins, readConfig func(string, string, any) error,
) {
	b := gatewayAssetBuilder{plugins: plugins, status: status, options: options, load: load, openStore: openStore, readConfig: readConfig}
	b.addPlugins(gateway)
	b.addProviders(providers)
	b.addModels(models)
	b.addMCPs(gateway)
	b.addCatalogs(gateway)
}

func (b gatewayAssetBuilder) add(parent *cobra.Command, use string, args cobra.PositionalArgs, action gatewayAction) *cobra.Command {
	cmd := &cobra.Command{Use: use, Args: args, Run: func(cmd *cobra.Command, args []string) {
		effective, err := b.load()
		if err != nil {
			return
		}
		store, err := b.openStore(effective)
		var result any
		if err == nil {
			result, err = action(cmd, args, store)
		}
		if err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), err)
			*b.status = 1
			return
		}
		if result != nil {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			if encoder.Encode(result) != nil {
				*b.status = 1
			}
		}
	}}
	parent.AddCommand(cmd)
	return cmd
}

func (b gatewayAssetBuilder) addPlugins(gateway *cobra.Command) {
	add, plugins := b.add, b.plugins
	pluginGroup := &cobra.Command{Use: "plugin", Short: "Inspect compiled gateway plugin behavior and defaults"}
	gateway.AddCommand(pluginGroup)
	add(pluginGroup, "list", cobra.NoArgs, func(*cobra.Command, []string, gatewaycontrol.AssetOperations) (any, error) { return plugins.List(), nil })
	var pluginName string
	showPlugin := add(pluginGroup, "show [PLUGIN]", selectorArgs(&pluginName), func(_ *cobra.Command, args []string, _ gatewaycontrol.AssetOperations) (any, error) {
		name, _ := selected(pluginName, args)
		return plugins.Find(name)
	})
	showPlugin.Flags().StringVar(&pluginName, "plugin", "", "Compiled plugin name")
}

func (b gatewayAssetBuilder) addProviders(providers *cobra.Command) {
	add, plugins := b.add, b.plugins
	add(providers, "list", cobra.NoArgs, func(_ *cobra.Command, _ []string, s gatewaycontrol.AssetOperations) (any, error) { return s.Providers() })
	var providerShow string
	showProvider := add(providers, "show [PROVIDER]", selectorArgs(&providerShow), func(_ *cobra.Command, args []string, s gatewaycontrol.AssetOperations) (any, error) {
		name, _ := selected(providerShow, args)
		return s.FindProvider(name)
	})
	showProvider.Flags().StringVar(&providerShow, "provider", "", "Provider instance name")
	var pluginApply, providerApply, providerConfig, providerFile string
	applyProvider := add(providers, "apply [PLUGIN]", selectorArgs(&pluginApply), func(_ *cobra.Command, args []string, s gatewaycontrol.AssetOperations) (any, error) {
		plugin, _ := selected(pluginApply, args)
		if _, err := plugins.Find(plugin); err != nil { return nil, err }
		var configuration gatewaycontrol.Configuration
		if err := b.readConfig(providerConfig, providerFile, &configuration); err != nil { return nil, err }
		return s.ApplyProviderInstance(plugin, providerApply, configuration)
	})
	applyProvider.Flags().StringVar(&pluginApply, "plugin", "", "Compiled plugin to instantiate")
	applyProvider.Flags().StringVar(&providerApply, "provider", "", "Instance name (defaults to plugin default)")
	configFlags(applyProvider, &providerConfig, &providerFile, false)
	var providerLogin string
	var enablePlan bool
	var consentApp, consentBackground bool
	login := add(providers, "login [PROVIDER]", cobra.MaximumNArgs(1), func(cmd *cobra.Command, args []string, s gatewaycontrol.AssetOperations) (any, error) {
		name, err := selected(providerLogin, args)
		if err != nil {
			return nil, err
		}
		return nil, s.LoginProvider(cmd.Context(), name, gatewaycontrol.LoginOptions{
			Output: cmd.OutOrStdout(),
			EnablePlan: enablePlan,
			ConsentConnectedApp: consentApp,
			ConsentBackground: consentBackground,
		})
	})
	login.Flags().StringVar(&providerLogin, "provider", "", "Provider instance to authenticate")
	login.Flags().BoolVar(&enablePlan, "enable-plan", false, "Explicitly request plan-usage consent when supported")
	login.Flags().BoolVar(&consentApp, "consent-connected-app", false, "Consent to connected Aginctus application use")
	login.Flags().BoolVar(&consentBackground, "consent-background", false, "Consent to background use by the user's controlled runtime")
	login.Args = func(cmd *cobra.Command, args []string) error {
		if err := cobra.MaximumNArgs(1)(cmd, args); err != nil {
			return err
		}
		_, err := selected(providerLogin, args)
		return err
	}
	for _, action := range []string{"status", "logout"} {
		var providerName string
		command := add(providers, action+" [PROVIDER]", selectorArgs(&providerName), func(cmd *cobra.Command, args []string, store gatewaycontrol.AssetOperations) (any, error) {
			name, _ := selected(providerName, args)
			return store.ProviderSession(cmd.Context(), name, action)
		})
		command.Flags().StringVar(&providerName, "provider", "", "Provider instance")
	}
}

func (b gatewayAssetBuilder) addModels(models *cobra.Command) {
	add := b.add
	var modelProvider string
	listModels := add(models, "list", cobra.NoArgs, func(cmd *cobra.Command, _ []string, s gatewaycontrol.AssetOperations) (any, error) {
		return s.ProviderModels(cmd.Context(), modelProvider)
	})
	listModels.Flags().StringVar(&modelProvider, "provider", "", "Filter by provider (may discover upstream models)")
	var showModelProvider, modelName string
	showModel := add(models, "show [PROVIDER] [MODEL]", cobra.MaximumNArgs(2), func(cmd *cobra.Command, args []string, s gatewaycontrol.AssetOperations) (any, error) {
		provider, name, err := modelSelection(showModelProvider, modelName, args)
		if err != nil {
			return nil, err
		}
		return s.SelectModel(cmd.Context(), provider, name)
	})
	showModel.Flags().StringVar(&showModelProvider, "provider", "", "Provider offering the model")
	showModel.Flags().StringVar(&modelName, "model", "", "Provider model name")
	showModel.Args = func(_ *cobra.Command, args []string) error {
		_, _, err := modelSelection(showModelProvider, modelName, args)
		return err
	}
}

func (b gatewayAssetBuilder) addMCPs(gateway *cobra.Command) {
	add := b.add
	mcps := &cobra.Command{Use: "mcp", Short: "Declare and inspect local or provider-offered MCP servers"}
	gateway.AddCommand(mcps)
	var mcpProvider string
	listMCP := add(mcps, "list", cobra.NoArgs, func(_ *cobra.Command, _ []string, s gatewaycontrol.AssetOperations) (any, error) {
		return s.MCPInventory(mcpProvider)
	})
	listMCP.Flags().StringVar(&mcpProvider, "provider", "", "Filter by provider")
	var showMCP, showMCPProvider string
	var localMCP bool
	mcpShow := add(mcps, "show [MCP]", selectorArgs(&showMCP), func(_ *cobra.Command, args []string, s gatewaycontrol.AssetOperations) (any, error) {
		name, _ := selected(showMCP, args)
		return s.SelectMCP(name, showMCPProvider, localMCP)
	})
	mcpShow.Flags().StringVar(&showMCP, "mcp", "", "MCP server name")
	mcpShow.Flags().StringVar(&showMCPProvider, "provider", "", "Provider offering this MCP")
	mcpShow.Flags().BoolVar(&localMCP, "local", false, "Select a local declaration")
	mcpShow.MarkFlagsMutuallyExclusive("provider", "local")
	var applyMCP, mcpConfig, mcpFile string
	var applyLocal bool
	mcpApply := add(mcps, "apply [MCP]", selectorArgs(&applyMCP), func(_ *cobra.Command, args []string, s gatewaycontrol.AssetOperations) (any, error) {
		name, _ := selected(applyMCP, args)
		var raw json.RawMessage
		if err := b.readConfig(mcpConfig, mcpFile, &raw); err != nil {
			return nil, err
		}
		return s.ApplyMCPDeclaration(name, raw)
	})
	mcpApply.Flags().StringVar(&applyMCP, "mcp", "", "Local MCP name")
	mcpApply.Flags().BoolVar(&applyLocal, "local", false, "Declare locally (the default)")
	configFlags(mcpApply, &mcpConfig, &mcpFile, true)
}

func (b gatewayAssetBuilder) addCatalogs(gateway *cobra.Command) {
	add := b.add
	catalogs := &cobra.Command{Use: "catalog", Short: "Select, inherit and rename models and MCP declarations"}
	gateway.AddCommand(catalogs)
	add(catalogs, "list", cobra.NoArgs, func(_ *cobra.Command, _ []string, s gatewaycontrol.AssetOperations) (any, error) {
		return s.CatalogNames()
	})
	var showCatalog, filterModel, filterMCP string
	catalogShow := add(catalogs, "show [CATALOG]", selectorArgs(&showCatalog), func(_ *cobra.Command, args []string, s gatewaycontrol.AssetOperations) (any, error) {
		name, _ := selected(showCatalog, args)
		return s.SelectCatalog(name, filterModel, filterMCP)
	})
	catalogShow.Flags().StringVar(&showCatalog, "catalog", "", "Catalog name")
	catalogShow.Flags().StringVar(&filterModel, "model", "", "Show models, optionally one exposed name")
	catalogShow.Flags().Lookup("model").NoOptDefVal = "*"
	catalogShow.Flags().StringVar(&filterMCP, "mcp", "", "Show MCPs, optionally one exposed name")
	catalogShow.Flags().Lookup("mcp").NoOptDefVal = "*"
	catalogShow.MarkFlagsMutuallyExclusive("model", "mcp")
	var applyCatalog, catalogConfig, catalogFile string
	catalogApply := add(catalogs, "apply [CATALOG]", selectorArgs(&applyCatalog), func(_ *cobra.Command, args []string, s gatewaycontrol.AssetOperations) (any, error) {
		name, _ := selected(applyCatalog, args)
		var catalog gatewaycontrol.Catalog
		if err := b.readConfig(catalogConfig, catalogFile, &catalog); err != nil {
			return nil, err
		}
		return s.ApplyCatalogDeclaration(name, catalog)
	})
	catalogApply.Flags().StringVar(&applyCatalog, "catalog", "", "Catalog name")
	configFlags(catalogApply, &catalogConfig, &catalogFile, true)
}

func selected(flag string, args []string) (string, error) {
	if len(args) > 1 || (flag != "" && len(args) > 0) {
		return "", fmt.Errorf("select a name either positionally or by flag, not both")
	}
	if len(args) == 1 {
		return args[0], nil
	}
	return flag, nil
}
func selectorArgs(flag *string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		name, err := selected(*flag, args)
		if err != nil {
			return err
		}
		if name == "" {
			return fmt.Errorf("a name is required positionally or by flag")
		}
		return nil
	}
}
func modelSelection(provider, model string, args []string) (string, string, error) {
	if len(args) > 2 {
		return "", "", fmt.Errorf("expected provider and model selectors")
	}
	if len(args) == 2 {
		if provider != "" || model != "" {
			return "", "", fmt.Errorf("duplicate provider/model selection")
		}
		provider, model = args[0], args[1]
	}
	if len(args) == 1 {
		if model == "" {
			model = args[0]
		} else if provider == "" {
			provider = args[0]
		} else {
			return "", "", fmt.Errorf("duplicate provider/model selection")
		}
	}
	if model == "" {
		return "", "", fmt.Errorf("a model name is required")
	}
	return provider, model, nil
}
func configFlags(cmd *cobra.Command, inline, file *string, required bool) {
	cmd.Flags().StringVar(inline, "config", "", "Nonsecret JSON configuration object")
	cmd.Flags().StringVar(file, "config-file", "", "JSON configuration file (at most 1 MiB)")
	cmd.MarkFlagsMutuallyExclusive("config", "config-file")
	if required {
		cmd.MarkFlagsOneRequired("config", "config-file")
	}
}
