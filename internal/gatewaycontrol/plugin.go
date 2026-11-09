// Package gatewaycontrol implements the local gateway asset control plane.
// Declarations do not deploy routes, execute MCP processes or grant workload access.
package gatewaycontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/mcpschema"
)

type Model struct {
	Name        string `json:"name"`
	Source      string `json:"source"`
	DisplayName string `json:"display_name,omitempty"`
}

type Configuration struct {
	StateDir string                        `json:"state_dir,omitempty"`
	Models   map[string]Model              `json:"models,omitempty"`
	MCPs     map[string]json.RawMessage    `json:"mcps,omitempty"`
	Routes   map[string]RouteConfiguration `json:"routes,omitempty"`
}

type RouteConfiguration struct {
	Catalog         string `json:"catalog"`
	WorkloadKeyFile string `json:"workload_key_file"`
}

type Provider struct {
	Name   string        `json:"name"`
	Plugin string        `json:"plugin"`
	Config Configuration `json:"config"`
}

type LoginOptions struct {
	Output              io.Writer
	EnablePlan          bool
	ConsentConnectedApp bool
	ConsentBackground   bool
}

// Plugin is compiled behavior, not a dynamically loaded executable or module.
// Optional hooks belong to an explicitly selected provider instance.
type Plugin struct {
	Name            string                                              `json:"name"`
	Description     string                                              `json:"description"`
	DefaultProvider string                                              `json:"default_provider"`
	Validate        func(Configuration) error                           `json:"-"`
	DiscoverModels  func(context.Context, Provider) ([]Model, error)    `json:"-"`
	Login           func(context.Context, Provider, LoginOptions) error `json:"-"`
	Status          func(context.Context, Provider) (any, error)        `json:"-"`
	Logout          func(context.Context, Provider) error               `json:"-"`
	RenderModel     func(Provider, Model) ([]byte, error)               `json:"-"`
}

type Plugins map[string]Plugin

func Builtins() Plugins {
	return Plugins{
		"chatgpt":     registrationPlugin("chatgpt", gateway.ChatGPTModelRegistration),
		"opencode-go": OpenCodeGoPlugin(),
	}
}

func registrationPlugin(name string, render func(string, string) ([]byte, error)) Plugin {
	return Plugin{Name: name, DefaultProvider: name,
		Description: "Local LiteLLM " + name + " declarations; authentication and activation are separate",
		Validate: func(c Configuration) error {
			if c.StateDir != "" {
				return fmt.Errorf("this plugin does not accept a state_dir")
			}
			for alias, model := range c.Models {
				if _, err := render(alias, model.Source); err != nil {
					return fmt.Errorf("model source is incompatible with the selected plugin")
				}
			}
			return nil
		},
		RenderModel: func(_ Provider, model Model) ([]byte, error) {
			return render(model.Name, model.Source)
		},
	}
}

func (p Plugins) List() []Plugin {
	names := make([]string, 0, len(p))
	for name := range p {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]Plugin, 0, len(p))
	for _, name := range names {
		result = append(result, p[name])
	}
	return result
}

func Decode(data []byte, result any) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return fmt.Errorf("configuration must be a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(result); err != nil {
		return fmt.Errorf("invalid configuration: expected the documented JSON object")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("configuration must contain exactly one JSON object")
	}
	return nil
}

var label = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
var exposed = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_./-]{0,127}$`)

func ValidName(name string) bool { return label.MatchString(name) }

func ValidateMCP(name string, data json.RawMessage) error {
	var value map[string]any
	if Decode(data, &value) != nil || value == nil {
		return fmt.Errorf("invalid MCP configuration object")
	}
	_, err := mcpschema.Parse(map[string]any{"servers": map[string]any{name: value}})
	if err != nil {
		return fmt.Errorf("invalid MCP declaration; use the existing credential-reference-only MCP schema")
	}
	// Parse validates the server shape; Resolve additionally requires gateway
	// routing intent, which a catalog declaration must not implicitly enable.
	return nil
}

func (plugins Plugins) Validate(p Provider) error {
	if len(p.Config.Routes) != 0 && p.Plugin != "openai-siwc" {
		return fmt.Errorf("route declarations require the openai-siwc plugin")
	}
	plugin, ok := plugins[p.Plugin]
	if !ok || !ValidName(p.Name) {
		return fmt.Errorf("unknown compiled plugin or invalid provider name")
	}
	if p.Config.StateDir != "" && (!filepath.IsAbs(p.Config.StateDir) || filepath.Clean(p.Config.StateDir) != p.Config.StateDir) {
		return fmt.Errorf("state_dir must be an absolute normalized private path")
	}
	for name, m := range p.Config.Models {
		if !exposed.MatchString(name) || m.Source == "" || (m.Name != "" && m.Name != name) {
			return fmt.Errorf("invalid provider model declaration")
		}
	}
	for name, mcp := range p.Config.MCPs {
		if err := ValidateMCP(name, mcp); err != nil {
			return err
		}
	}
	if plugin.Validate != nil {
		return plugin.Validate(p.Config)
	}
	return nil
}
