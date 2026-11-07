package gatewaycontrol

import (
	"encoding/json"
	"fmt"
	"sort"
)

type Asset struct {
	Provider string `json:"provider,omitempty"`
	Name     string `json:"name"`
	Local    bool   `json:"local,omitempty"`
}

// Import selects source alias -> exposed alias mappings. Omitted selections
// import nothing, so future parent assets cannot silently widen a child catalog.
type Import struct {
	Catalog string            `json:"catalog"`
	Models  map[string]string `json:"models,omitempty"`
	MCPs    map[string]string `json:"mcps,omitempty"`
}

type Catalog struct {
	Inherits []Import         `json:"inherits,omitempty"`
	Models   map[string]Asset `json:"models,omitempty"`
	MCPs     map[string]Asset `json:"mcps,omitempty"`
}

type Assets struct {
	MCPs     map[string]json.RawMessage `json:"mcps"`
	Catalogs map[string]Catalog         `json:"catalogs"`
}

type Resolved struct {
	Models map[string]Asset `json:"models"`
	MCPs   map[string]Asset `json:"mcps"`
}

func Resolve(name string, providers map[string]Provider, assets Assets) (Resolved, error) {
	return resolve(name, providers, assets, map[string]bool{}, map[string]Resolved{})
}

func resolve(name string, providers map[string]Provider, assets Assets, visiting map[string]bool, cache map[string]Resolved) (Resolved, error) {
	if cached, ok := cache[name]; ok {
		return cached, nil
	}
	result := Resolved{Models: map[string]Asset{}, MCPs: map[string]Asset{}}
	catalog, ok := assets.Catalogs[name]
	if !ok || !ValidName(name) {
		return result, fmt.Errorf("unknown or invalid catalog")
	}
	if visiting[name] || len(visiting) >= 64 {
		return result, fmt.Errorf("catalog inheritance cycle")
	}
	visiting[name] = true
	defer delete(visiting, name)
	for _, selection := range catalog.Inherits {
		parent, err := resolve(selection.Catalog, providers, assets, visiting, cache)
		if err != nil {
			return result, err
		}
		for _, group := range []struct {
			mapping        map[string]string
			source, target map[string]Asset
		}{{selection.Models, parent.Models, result.Models}, {selection.MCPs, parent.MCPs, result.MCPs}} {
			for from, to := range group.mapping {
				asset, ok := group.source[from]
				if !ok {
					return result, fmt.Errorf("inherited asset does not exist")
				}
				if err := insert(group.target, to, asset); err != nil {
					return result, err
				}
			}
		}
	}
	for alias, asset := range catalog.Models {
		provider, ok := providers[asset.Provider]
		if !ok || asset.Local || provider.Config.Models[asset.Name].Source == "" {
			return result, fmt.Errorf("catalog references an undeclared provider model")
		}
		if err := insert(result.Models, alias, asset); err != nil {
			return result, err
		}
	}
	for alias, asset := range catalog.MCPs {
		if asset.Local {
			if asset.Provider != "" || assets.MCPs[asset.Name] == nil {
				return result, fmt.Errorf("catalog references an undeclared local MCP")
			}
		} else if providers[asset.Provider].Config.MCPs[asset.Name] == nil {
			return result, fmt.Errorf("catalog references an undeclared provider MCP")
		}
		if err := insert(result.MCPs, alias, asset); err != nil {
			return result, err
		}
	}
	cache[name] = result
	return result, nil
}

func insert(target map[string]Asset, name string, asset Asset) error {
	if !exposed.MatchString(name) {
		return fmt.Errorf("invalid exposed asset name")
	}
	if _, exists := target[name]; exists {
		return fmt.Errorf("catalog asset name collision; rename selections explicitly")
	}
	target[name] = asset
	return nil
}

func ValidateAssets(providers map[string]Provider, assets Assets) error {
	for name, mcp := range assets.MCPs {
		if err := ValidateMCP(name, mcp); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(assets.Catalogs))
	for name := range assets.Catalogs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := Resolve(name, providers, assets); err != nil {
			return err
		}
	}
	return nil
}
