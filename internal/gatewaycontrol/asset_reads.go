package gatewaycontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// FindProvider resolves a registered instance without granting any provider
// operation or inferring authority from a declaration.
func (s *Store) FindProvider(name string) (Provider, error) {
	all, err := s.Providers()
	if err != nil { return Provider{}, err }
	p, ok := all[name]
	if !ok { return p, fmt.Errorf("provider is not registered") }
	return p, nil
}

// ProviderModels returns declared models by default; explicit provider
// discovery can consult the selected plugin, but never changes local state.
func (s *Store) ProviderModels(ctx context.Context, filter string) (map[string][]Model, error) {
	providers, err := s.Providers()
	if err != nil { return nil, err }
	result := map[string][]Model{}
	if filter != "" {
		if _, ok := providers[filter]; !ok { return nil, fmt.Errorf("unknown provider") }
	}
	for name, p := range providers {
		if filter != "" && name != filter { continue }
		plugin := s.Plugins[p.Plugin]
		models := []Model{}
		if filter != "" && plugin.DiscoverModels != nil {
			models, err = plugin.DiscoverModels(ctx, p)
			if err != nil { return nil, err }
		} else {
			for alias, model := range p.Config.Models {
				model.Name = alias
				models = append(models, model)
			}
		}
		sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
		result[name] = models
	}
	return result, nil
}

// SelectModel rejects missing and ambiguous exposed names rather than
// silently selecting one provider from an unordered map.
func (s *Store) SelectModel(ctx context.Context, provider, name string) (map[string]Model, error) {
	all, err := s.ProviderModels(ctx, provider)
	if err != nil { return nil, err }
	found := map[string]Model{}
	for p, models := range all {
		for _, model := range models {
			if model.Name == name { found[p] = model }
		}
	}
	if len(found) != 1 { return nil, fmt.Errorf("model missing or ambiguous; select its provider explicitly") }
	return found, nil
}

// MCPInventory preserves the distinct local and provider-offered namespaces.
// The reserved provider name "local" does not override the local declaration.
func (s *Store) MCPInventory(provider string) (map[string]any, error) {
	assets, err := s.Assets()
	if err != nil { return nil, err }
	providers, err := s.Providers()
	if err != nil { return nil, err }
	offered := map[string]map[string]json.RawMessage{}
	result := map[string]any{"providers": offered}
	if provider == "" { result["local"] = assets.MCPs }
	if provider != "" {
		if _, ok := providers[provider]; !ok { return nil, fmt.Errorf("unknown provider") }
	}
	for name, instance := range providers {
		if provider == "" || name == provider { offered[name] = instance.Config.MCPs }
	}
	return result, nil
}

// SelectMCP resolves a provider-offered MCP only when selected explicitly.
func (s *Store) SelectMCP(name, provider string, local bool) (json.RawMessage, error) {
	if !local && provider != "" {
		p, err := s.FindProvider(provider)
		if err != nil { return nil, err }
		if p.Config.MCPs[name] == nil { return nil, fmt.Errorf("unknown provider MCP") }
		return p.Config.MCPs[name], nil
	}
	assets, err := s.Assets()
	if err != nil { return nil, err }
	if assets.MCPs[name] == nil { return nil, fmt.Errorf("unknown local MCP") }
	return assets.MCPs[name], nil
}

func (s *Store) CatalogNames() ([]string, error) {
	assets, err := s.Assets()
	if err != nil { return nil, err }
	names := []string{}
	for name := range assets.Catalogs { names = append(names, name) }
	sort.Strings(names)
	return names, nil
}

// SelectCatalog resolves and filters already-declared model/MCP mappings.
// A read-only projection must never activate routing or provider login.
func (s *Store) SelectCatalog(name, model, mcp string) (any, error) {
	assets, err := s.Assets()
	if err != nil { return nil, err }
	providers, err := s.Providers()
	if err != nil { return nil, err }
	resolved, err := Resolve(name, providers, assets)
	if err != nil { return nil, err }
	if model != "" {
		if model == "*" { return resolved.Models, nil }
		value, ok := resolved.Models[model]
		if !ok { return nil, fmt.Errorf("unknown exposed model") }
		return value, nil
	}
	if mcp != "" {
		if mcp == "*" { return resolved.MCPs, nil }
		value, ok := resolved.MCPs[mcp]
		if !ok { return nil, fmt.Errorf("unknown exposed MCP") }
		return value, nil
	}
	return resolved, nil
}
