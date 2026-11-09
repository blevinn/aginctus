package assetscmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"syscall"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gatewaycontrol"
	"github.com/spf13/cobra"
)

type gatewayAction func(*cobra.Command, []string, *gatewaycontrol.Store) (any, error)

type gatewayAssetBuilder struct {
	plugins gatewaycontrol.Plugins
	status  *int
	options config.Options
	load    func() (*config.Config, error)
	openStore func(*config.Config) (*gatewaycontrol.Store, error)
}

// Register contributes the plugin/provider/model/MCP/catalog subtrees. Store
// opening stays behind an injected trusted host boundary rather than importing cli.
func Register(gateway, providers, models *cobra.Command, status *int, options config.Options,
	load func() (*config.Config, error), openStore func(*config.Config) (*gatewaycontrol.Store, error),
) {
	b := gatewayAssetBuilder{plugins: gatewaycontrol.Builtins(), status: status, options: options, load: load, openStore: openStore}
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
	add(pluginGroup, "list", cobra.NoArgs, func(*cobra.Command, []string, *gatewaycontrol.Store) (any, error) { return plugins.List(), nil })
	var pluginName string
	showPlugin := add(pluginGroup, "show [PLUGIN]", selectorArgs(&pluginName), func(_ *cobra.Command, args []string, _ *gatewaycontrol.Store) (any, error) {
		name, _ := selected(pluginName, args)
		plugin, ok := plugins[name]
		if !ok {
			return nil, fmt.Errorf("unknown compiled plugin")
		}
		return plugin, nil
	})
	showPlugin.Flags().StringVar(&pluginName, "plugin", "", "Compiled plugin name")
}

func (b gatewayAssetBuilder) addProviders(providers *cobra.Command) {
	add, plugins := b.add, b.plugins
	add(providers, "list", cobra.NoArgs, func(_ *cobra.Command, _ []string, s *gatewaycontrol.Store) (any, error) { return s.Providers() })
	var providerShow string
	showProvider := add(providers, "show [PROVIDER]", selectorArgs(&providerShow), func(_ *cobra.Command, args []string, s *gatewaycontrol.Store) (any, error) {
		name, _ := selected(providerShow, args)
		return findProvider(s, name)
	})
	showProvider.Flags().StringVar(&providerShow, "provider", "", "Provider instance name")
	var pluginApply, providerApply, providerConfig, providerFile string
	applyProvider := add(providers, "apply [PLUGIN]", selectorArgs(&pluginApply), func(_ *cobra.Command, args []string, s *gatewaycontrol.Store) (any, error) {
		plugin, _ := selected(pluginApply, args)
		definition, ok := plugins[plugin]
		if !ok {
			return nil, fmt.Errorf("unknown compiled plugin")
		}
		name := providerApply
		if name == "" {
			name = definition.DefaultProvider
		}
		var configuration gatewaycontrol.Configuration
		if err := readGatewayConfig(providerConfig, providerFile, &configuration); err != nil {
			return nil, err
		}
		provider := gatewaycontrol.Provider{Name: name, Plugin: plugin, Config: configuration}
		if err := s.ApplyProvider(provider); err != nil {
			return nil, err
		}
		return provider, nil
	})
	applyProvider.Flags().StringVar(&pluginApply, "plugin", "", "Compiled plugin to instantiate")
	applyProvider.Flags().StringVar(&providerApply, "provider", "", "Instance name (defaults to plugin default)")
	configFlags(applyProvider, &providerConfig, &providerFile, false)
	var providerLogin string
	var enablePlan bool
	login := add(providers, "login [PROVIDER]", cobra.MaximumNArgs(1), func(cmd *cobra.Command, args []string, s *gatewaycontrol.Store) (any, error) {
		name, err := selected(providerLogin, args)
		if err != nil {
			return nil, err
		}
		if name == "" {
			all, err := s.Providers()
			if err != nil {
				return nil, err
			}
			if len(all) != 1 {
				return nil, fmt.Errorf("select --provider explicitly unless exactly one instance exists")
			}
			for key := range all {
				name = key
			}
		}
		p, err := findProvider(s, name)
		if err != nil {
			return nil, err
		}
		plugin := plugins[p.Plugin]
		if plugin.Login == nil {
			return nil, fmt.Errorf("this plugin does not implement interactive login")
		}
		return nil, plugin.Login(cmd.Context(), p, gatewaycontrol.LoginOptions{Output: cmd.OutOrStdout(), EnablePlan: enablePlan})
	})
	login.Flags().StringVar(&providerLogin, "provider", "", "Provider instance to authenticate")
	login.Flags().BoolVar(&enablePlan, "enable-plan", false, "Explicitly request plan-usage consent when supported")
	login.Args = func(cmd *cobra.Command, args []string) error {
		if err := cobra.MaximumNArgs(1)(cmd, args); err != nil {
			return err
		}
		_, err := selected(providerLogin, args)
		return err
	}
}

func (b gatewayAssetBuilder) addModels(models *cobra.Command) {
	add := b.add
	var modelProvider string
	listModels := add(models, "list", cobra.NoArgs, func(cmd *cobra.Command, _ []string, s *gatewaycontrol.Store) (any, error) {
		return providerModels(cmd, s, modelProvider)
	})
	listModels.Flags().StringVar(&modelProvider, "provider", "", "Filter by provider (may discover upstream models)")
	var showModelProvider, modelName string
	showModel := add(models, "show [PROVIDER] [MODEL]", cobra.MaximumNArgs(2), func(cmd *cobra.Command, args []string, s *gatewaycontrol.Store) (any, error) {
		provider, name, err := modelSelection(showModelProvider, modelName, args)
		if err != nil {
			return nil, err
		}
		all, err := providerModels(cmd, s, provider)
		if err != nil {
			return nil, err
		}
		found := map[string]gatewaycontrol.Model{}
		for p, items := range all {
			for _, m := range items {
				if m.Name == name {
					found[p] = m
				}
			}
		}
		if len(found) != 1 {
			return nil, fmt.Errorf("model missing or ambiguous; select its provider explicitly")
		}
		return found, nil
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
	listMCP := add(mcps, "list", cobra.NoArgs, func(_ *cobra.Command, _ []string, s *gatewaycontrol.Store) (any, error) {
		assets, err := s.Assets()
		if err != nil {
			return nil, err
		}
		p, err := s.Providers()
		if err != nil {
			return nil, err
		}
		offered := map[string]map[string]json.RawMessage{}
		result := map[string]any{"providers": offered}
		if mcpProvider == "" {
			result["local"] = assets.MCPs
		}
		if mcpProvider != "" {
			if _, ok := p[mcpProvider]; !ok {
				return nil, fmt.Errorf("unknown provider")
			}
		}
		for name, instance := range p {
			if mcpProvider == "" || name == mcpProvider {
				offered[name] = instance.Config.MCPs
			}
		}
		return result, nil
	})
	listMCP.Flags().StringVar(&mcpProvider, "provider", "", "Filter by provider")
	var showMCP, showMCPProvider string
	var localMCP bool
	mcpShow := add(mcps, "show [MCP]", selectorArgs(&showMCP), func(_ *cobra.Command, args []string, s *gatewaycontrol.Store) (any, error) {
		name, _ := selected(showMCP, args)
		if !localMCP && showMCPProvider != "" {
			p, err := findProvider(s, showMCPProvider)
			if err != nil {
				return nil, err
			}
			if p.Config.MCPs[name] == nil {
				return nil, fmt.Errorf("unknown provider MCP")
			}
			return p.Config.MCPs[name], nil
		}
		assets, err := s.Assets()
		if err != nil {
			return nil, err
		}
		if assets.MCPs[name] == nil {
			return nil, fmt.Errorf("unknown local MCP")
		}
		return assets.MCPs[name], nil
	})
	mcpShow.Flags().StringVar(&showMCP, "mcp", "", "MCP server name")
	mcpShow.Flags().StringVar(&showMCPProvider, "provider", "", "Provider offering this MCP")
	mcpShow.Flags().BoolVar(&localMCP, "local", false, "Select a local declaration")
	mcpShow.MarkFlagsMutuallyExclusive("provider", "local")
	var applyMCP, mcpConfig, mcpFile string
	var applyLocal bool
	mcpApply := add(mcps, "apply [MCP]", selectorArgs(&applyMCP), func(_ *cobra.Command, args []string, s *gatewaycontrol.Store) (any, error) {
		name, _ := selected(applyMCP, args)
		var raw json.RawMessage
		if err := readGatewayConfig(mcpConfig, mcpFile, &raw); err != nil {
			return nil, err
		}
		if err := s.ApplyMCP(name, raw); err != nil {
			return nil, err
		}
		return raw, nil
	})
	mcpApply.Flags().StringVar(&applyMCP, "mcp", "", "Local MCP name")
	mcpApply.Flags().BoolVar(&applyLocal, "local", false, "Declare locally (the default)")
	configFlags(mcpApply, &mcpConfig, &mcpFile, true)
}

func (b gatewayAssetBuilder) addCatalogs(gateway *cobra.Command) {
	add := b.add
	catalogs := &cobra.Command{Use: "catalog", Short: "Select, inherit and rename models and MCP declarations"}
	gateway.AddCommand(catalogs)
	add(catalogs, "list", cobra.NoArgs, func(_ *cobra.Command, _ []string, s *gatewaycontrol.Store) (any, error) {
		assets, err := s.Assets()
		if err != nil {
			return nil, err
		}
		names := []string{}
		for name := range assets.Catalogs {
			names = append(names, name)
		}
		sort.Strings(names)
		return names, nil
	})
	var showCatalog, filterModel, filterMCP string
	catalogShow := add(catalogs, "show [CATALOG]", selectorArgs(&showCatalog), func(_ *cobra.Command, args []string, s *gatewaycontrol.Store) (any, error) {
		name, _ := selected(showCatalog, args)
		assets, err := s.Assets()
		if err != nil {
			return nil, err
		}
		providers, err := s.Providers()
		if err != nil {
			return nil, err
		}
		resolved, err := gatewaycontrol.Resolve(name, providers, assets)
		if err != nil {
			return nil, err
		}
		if filterModel != "" {
			if filterModel == "*" {
				return resolved.Models, nil
			}
			value, ok := resolved.Models[filterModel]
			if !ok {
				return nil, fmt.Errorf("unknown exposed model")
			}
			return value, nil
		}
		if filterMCP != "" {
			if filterMCP == "*" {
				return resolved.MCPs, nil
			}
			value, ok := resolved.MCPs[filterMCP]
			if !ok {
				return nil, fmt.Errorf("unknown exposed MCP")
			}
			return value, nil
		}
		return resolved, nil
	})
	catalogShow.Flags().StringVar(&showCatalog, "catalog", "", "Catalog name")
	catalogShow.Flags().StringVar(&filterModel, "model", "", "Show models, optionally one exposed name")
	catalogShow.Flags().Lookup("model").NoOptDefVal = "*"
	catalogShow.Flags().StringVar(&filterMCP, "mcp", "", "Show MCPs, optionally one exposed name")
	catalogShow.Flags().Lookup("mcp").NoOptDefVal = "*"
	catalogShow.MarkFlagsMutuallyExclusive("model", "mcp")
	var applyCatalog, catalogConfig, catalogFile string
	catalogApply := add(catalogs, "apply [CATALOG]", selectorArgs(&applyCatalog), func(_ *cobra.Command, args []string, s *gatewaycontrol.Store) (any, error) {
		name, _ := selected(applyCatalog, args)
		var catalog gatewaycontrol.Catalog
		if err := readGatewayConfig(catalogConfig, catalogFile, &catalog); err != nil {
			return nil, err
		}
		if err := s.ApplyCatalog(name, catalog); err != nil {
			return nil, err
		}
		return catalog, nil
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
func readGatewayConfig(inline, file string, result any) error {
	data := []byte(inline)
	if file != "" {
		input, err := os.OpenFile(file, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return fmt.Errorf("configuration file unavailable")
		}
		defer input.Close()
		info, err := input.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("configuration must be a regular file")
		}
		data, err = io.ReadAll(io.LimitReader(input, 1<<20+1))
		if err != nil {
			return fmt.Errorf("configuration file unavailable")
		}
	}
	if len(data) == 0 {
		data = []byte(`{}`)
	}
	if len(data) > 1<<20 {
		return fmt.Errorf("configuration exceeds size limit")
	}
	return gatewaycontrol.Decode(data, result)
}
func findProvider(store *gatewaycontrol.Store, name string) (gatewaycontrol.Provider, error) {
	all, err := store.Providers()
	if err != nil {
		return gatewaycontrol.Provider{}, err
	}
	p, ok := all[name]
	if !ok {
		return p, fmt.Errorf("provider is not registered")
	}
	return p, nil
}
func providerModels(cmd *cobra.Command, store *gatewaycontrol.Store, filter string) (map[string][]gatewaycontrol.Model, error) {
	providers, err := store.Providers()
	if err != nil {
		return nil, err
	}
	result := map[string][]gatewaycontrol.Model{}
	if filter != "" {
		if _, ok := providers[filter]; !ok {
			return nil, fmt.Errorf("unknown provider")
		}
	}
	for name, p := range providers {
		if filter != "" && name != filter {
			continue
		}
		plugin := store.Plugins[p.Plugin]
		models := []gatewaycontrol.Model{}
		if filter != "" && plugin.DiscoverModels != nil {
			models, err = plugin.DiscoverModels(cmd.Context(), p)
			if err != nil {
				return nil, err
			}
		} else {
			for alias, m := range p.Config.Models {
				m.Name = alias
				models = append(models, m)
			}
		}
		sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
		result[name] = models
	}
	return result, nil
}
