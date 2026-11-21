package gatewaycontrol

import (
	"context"
	"fmt"
	"encoding/json"
)

// AssetOperations is the narrow command-facing gateway control capability.
// Cobra registration may select an action but must not own persistence,
// provider discovery or authentication policy.
type AssetOperations interface {
	Providers() (map[string]Provider, error)
	FindProvider(string) (Provider, error)
	ProviderModels(context.Context, string) (map[string][]Model, error)
	SelectModel(context.Context, string, string) (map[string]Model, error)
	MCPInventory(string) (map[string]any, error)
	SelectMCP(string, string, bool) (json.RawMessage, error)
	CatalogNames() ([]string, error)
	SelectCatalog(string, string, string) (any, error)
	ApplyProviderInstance(string, string, Configuration) (Provider, error)
	LoginProvider(context.Context, string, LoginOptions) error
	ProviderSession(context.Context, string, string) (any, error)
	ApplyMCPDeclaration(string, []byte) (json.RawMessage, error)
	ApplyCatalogDeclaration(string, Catalog) (Catalog, error)
}

var _ AssetOperations = (*Store)(nil)

// ApplyProviderInstance validates the selected compiled plugin and resolves
// its default name before any local declaration is persisted.
func (s *Store) ApplyProviderInstance(pluginName, name string, configuration Configuration) (Provider, error) {
	definition, ok := s.Plugins[pluginName]
	if !ok { return Provider{}, fmt.Errorf("unknown compiled plugin") }
	if name == "" { name = definition.DefaultProvider }
	provider := Provider{Name: name, Plugin: pluginName, Config: configuration}
	if err := s.ApplyProvider(provider); err != nil { return Provider{}, err }
	return provider, nil
}

// ResolveLoginProvider requires an unambiguous instance. A name supplied by a
// caller does not grant authentication or override the plugin's consent policy.
func (s *Store) ResolveLoginProvider(name string) (Provider, error) {
	if name == "" {
		all, err := s.Providers()
		if err != nil { return Provider{}, err }
		if len(all) != 1 { return Provider{}, fmt.Errorf("select --provider explicitly unless exactly one instance exists") }
		for candidate := range all { name = candidate }
	}
	return s.FindProvider(name)
}

// LoginProvider calls the selected provider's interactive login capability.
// The caller explicitly supplies output and optional consent flags; this API
// does not silently enable connected-app or background grants.
func (s *Store) LoginProvider(ctx context.Context, name string, options LoginOptions) error {
	provider, err := s.ResolveLoginProvider(name)
	if err != nil { return err }
	plugin := s.Plugins[provider.Plugin]
	if plugin.Login == nil { return fmt.Errorf("this plugin does not implement interactive login") }
	return plugin.Login(ctx, provider, options)
}

// ProviderSession operates only on the named, registered instance. It cannot
// promote a status request to login or mutate a registry entry.
func (s *Store) ProviderSession(ctx context.Context, name, action string) (any, error) {
	provider, err := s.FindProvider(name)
	if err != nil { return nil, err }
	plugin := s.Plugins[provider.Plugin]
	if action == "status" && plugin.Status != nil { return plugin.Status(ctx, provider) }
	if action == "logout" && plugin.Logout != nil { return nil, plugin.Logout(ctx, provider) }
	return nil, fmt.Errorf("this plugin does not implement provider %s", action)
}

func (s *Store) ApplyMCPDeclaration(name string, configuration []byte) (json.RawMessage, error) {
	raw := json.RawMessage(configuration)
	if err := s.ApplyMCP(name, raw); err != nil { return nil, err }
	return raw, nil
}

func (s *Store) ApplyCatalogDeclaration(name string, catalog Catalog) (Catalog, error) {
	if err := s.ApplyCatalog(name, catalog); err != nil { return Catalog{}, err }
	return catalog, nil
}
