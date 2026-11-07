package gatewaycontrol

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"github.com/blevinn/aginctus/internal/filestate"
	"github.com/blevinn/aginctus/internal/modelregistry"
	"github.com/blevinn/aginctus/internal/providerregistry"
)

type Store struct {
	Directory string
	Plugins   Plugins
	Declared  Declarations
}

func DefaultStore(plugins Plugins) (*Store, error) {
	path, err := providerregistry.DefaultPath()
	if err != nil {
		return nil, err
	}
	return &Store{Directory: filepath.Dir(path), Plugins: plugins}, nil
}

func (s *Store) Providers() (map[string]Provider, error) {
	stored, err := providerregistry.Load(filepath.Join(s.Directory, "providers.json"))
	if err != nil {
		return nil, err
	}
	result := map[string]Provider{}
	for _, entry := range stored.Providers {
		p := Provider{Name: entry.Alias, Plugin: entry.Type}
		if entry.Config != nil {
			if err := Decode(*entry.Config, &p.Config); err != nil {
				return nil, err
			}
		}
		if err := s.Plugins.Validate(p); err != nil {
			return nil, err
		}
		if p.Config.Models == nil {
			p.Config.Models = map[string]Model{}
		}
		result[p.Name] = p
	}
	for name, definition := range s.Declared.Providers {
		if _, exists := result[name]; exists {
			return nil, fmt.Errorf("provider name conflicts with configuration")
		}
		result[name] = Provider{Name: name, Plugin: definition.Plugin, Config: definition.Config}
	}
	if err := s.addLegacyModels(result); err != nil {
		return nil, err
	}
	return result, nil
}

// Existing model intents remain visible without copying or rewriting state.
func (s *Store) addLegacyModels(result map[string]Provider) error {
	models, err := modelregistry.Load(filepath.Join(s.Directory, "models.json"))
	if err != nil {
		return err
	}
	for _, m := range models.Models {
		p, ok := result[m.Provider]
		if !ok {
			return fmt.Errorf("legacy model provider identity does not match any declared provider")
		}
		if configured, exists := p.Config.Models[m.Name]; exists && configured.Source != m.Source {
			return fmt.Errorf("model declaration conflicts with a legacy registration")
		}
		if _, declared := s.Declared.Providers[m.Provider]; declared {
			if _, configured := p.Config.Models[m.Name]; !configured {
				return fmt.Errorf("legacy registration cannot extend a configured provider")
			}
		}
		if p.Config.Models == nil {
			p.Config.Models = map[string]Model{}
		}
		p.Config.Models[m.Name] = Model{Name: m.Name, Source: m.Source}
		result[p.Name] = p
	}
	return nil
}

func (s *Store) Assets() (Assets, error) {
	empty := Assets{MCPs: map[string]json.RawMessage{}, Catalogs: map[string]Catalog{}}
	data, err := filestate.ReadPrivateFile(filepath.Join(s.Directory, "assets.json"), 1<<20)
	if errors.Is(err, os.ErrNotExist) {
		return s.mergeDeclaredAssets(empty)
	}
	if err != nil {
		return empty, err
	}
	if err := Decode(data, &empty); err != nil {
		return empty, err
	}
	if empty.MCPs == nil || empty.Catalogs == nil {
		return empty, fmt.Errorf("invalid gateway assets registry")
	}
	for name, mcp := range empty.MCPs {
		if err := ValidateMCP(name, mcp); err != nil {
			return empty, err
		}
	}
	return s.mergeDeclaredAssets(empty)
}

func (s *Store) update(action func(map[string]Provider, *Assets) error) error {
	if !filepath.IsAbs(s.Directory) {
		return fmt.Errorf("gateway registry directory must be absolute")
	}
	if err := os.MkdirAll(s.Directory, 0700); err != nil {
		return err
	}
	lock, err := filestate.LockRegistry(filepath.Join(s.Directory, "assets.json"))
	if err != nil {
		return err
	}
	defer lock.Close()
	providers, err := s.Providers()
	if err != nil {
		return err
	}
	assets, err := s.Assets()
	if err != nil {
		return err
	}
	return action(providers, &assets)
}

func (s *Store) ApplyProvider(p Provider) error {
	if _, declared := s.Declared.Providers[p.Name]; declared {
		return fmt.Errorf("provider is configuration-owned; edit its declaration")
	}
	if err := s.Plugins.Validate(p); err != nil {
		return err
	}
	return s.update(func(providers map[string]Provider, assets *Assets) error {
		if existing, ok := providers[p.Name]; ok && (existing.Plugin != p.Plugin || existing.Config.StateDir != p.Config.StateDir) {
			return fmt.Errorf("provider plugin and credential state identity are immutable; use a new provider name")
		}
		effective := p
		effective.Config.Models = maps.Clone(p.Config.Models)
		providers[p.Name] = effective
		if err := s.addLegacyModels(providers); err != nil {
			return err
		}
		if err := ValidateAssets(providers, *assets); err != nil {
			return err
		}
		data, err := json.Marshal(p.Config)
		if err != nil {
			return err
		}
		raw := json.RawMessage(data)
		return providerregistry.Apply(filepath.Join(s.Directory, "providers.json"), providerregistry.Provider{Type: p.Plugin, Alias: p.Name, Config: &raw})
	})
}

func (s *Store) ApplyMCP(name string, data json.RawMessage) error {
	if _, declared := s.Declared.MCPs[name]; declared {
		return fmt.Errorf("MCP is configuration-owned; edit its declaration")
	}
	if err := ValidateMCP(name, data); err != nil {
		return err
	}
	return s.update(func(providers map[string]Provider, assets *Assets) error {
		assets.MCPs[name] = data
		return s.write(providers, *assets)
	})
}

func (s *Store) ApplyCatalog(name string, catalog Catalog) error {
	if _, declared := s.Declared.Catalogs[name]; declared {
		return fmt.Errorf("catalog is configuration-owned; edit its declaration")
	}
	if !ValidName(name) {
		return fmt.Errorf("invalid catalog name")
	}
	return s.update(func(providers map[string]Provider, assets *Assets) error {
		assets.Catalogs[name] = catalog
		return s.write(providers, *assets)
	})
}

func (s *Store) write(providers map[string]Provider, assets Assets) error {
	if err := ValidateAssets(providers, assets); err != nil {
		return err
	}
	// Validate the combined view, but persist only locally managed declarations.
	for name := range s.Declared.Catalogs {
		delete(assets.Catalogs, name)
	}
	for name := range s.Declared.MCPs {
		delete(assets.MCPs, name)
	}
	data, err := json.MarshalIndent(assets, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > 1<<20 {
		return fmt.Errorf("gateway assets registry exceeds size limit")
	}
	return filestate.WritePrivateFile(filepath.Join(s.Directory, "assets.json"), append(data, '\n'))
}
