package gatewaycontrol

import (
	"encoding/json"
	"fmt"
)

type ProviderDefinition struct {
	Plugin string        `json:"plugin"`
	Config Configuration `json:"config"`
}

type Declarations struct {
	Providers map[string]ProviderDefinition `json:"providers"`
	Catalogs  map[string]Catalog            `json:"catalogs"`
	MCPs      map[string]json.RawMessage    `json:"mcps"`
}

// Configure selects nonsecret declarations without importing them into writable
// state. MCPs are the original mcp.servers identities, not catalog aliases.
func (s *Store) Configure(providers, catalogs, mcps any) error {
	data, err := json.Marshal(map[string]any{"providers": providers, "catalogs": catalogs, "mcps": mcps})
	if err != nil {
		return fmt.Errorf("invalid gateway declarations")
	}
	var declared Declarations
	if err := Decode(data, &declared); err != nil {
		return err
	}
	for name, definition := range declared.Providers {
		if err := s.Plugins.Validate(Provider{Name: name, Plugin: definition.Plugin, Config: definition.Config}); err != nil {
			return err
		}
	}
	s.Declared = declared
	selected, err := s.Providers()
	if err != nil {
		return err
	}
	assets, err := s.Assets()
	if err != nil {
		return err
	}
	return ValidateAssets(selected, assets)
}

func (s *Store) mergeDeclaredAssets(assets Assets) (Assets, error) {
	for name, mcp := range s.Declared.MCPs {
		if _, exists := assets.MCPs[name]; exists {
			return assets, fmt.Errorf("local MCP name conflicts with configuration")
		}
		if err := ValidateMCP(name, mcp); err != nil {
			return assets, err
		}
		assets.MCPs[name] = mcp
	}
	for name, catalog := range s.Declared.Catalogs {
		if _, exists := assets.Catalogs[name]; exists {
			return assets, fmt.Errorf("catalog name conflicts with configuration")
		}
		assets.Catalogs[name] = catalog
	}
	return assets, nil
}
