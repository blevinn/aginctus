// Package builtin is a synthetic build-selection fixture, not a shipped product.
package builtin

import (
	"context"
	_ "embed"
	"encoding/json"
	"strings"

	"github.com/blevinn/aginctus/pkg/plugin"
)

//go:embed capability.txt
var capability string

type implementation struct {
	name, api    string
	dependencies []string
}

func (p implementation) Descriptor() plugin.Descriptor {
	return plugin.Descriptor{APIVersion: p.api, Name: p.name, Version: "fixture-1",
		Capabilities: []plugin.Capability{{Name: strings.TrimSpace(capability), Kind: plugin.MCP}},
		Dependencies: p.dependencies}
}

func (implementation) ValidateConfiguration(context.Context, json.RawMessage) error {
	panic("build inventory must not instantiate runtime configuration")
}

func New() plugin.Plugin          { return implementation{name: "a-builtin", api: plugin.APIVersion} }
func NewDuplicate() plugin.Plugin { return New() }
func NewDependent() plugin.Plugin {
	return implementation{name: "a-builtin", api: plugin.APIVersion, dependencies: []string{"external-example"}}
}
func NewIncompatible() plugin.Plugin { return implementation{name: "a-builtin", api: "future"} }
func NewCycleA() plugin.Plugin {
	return implementation{name: "a-builtin", api: plugin.APIVersion, dependencies: []string{"cycle-peer"}}
}
func NewCycleB() plugin.Plugin {
	return implementation{name: "cycle-peer", api: plugin.APIVersion, dependencies: []string{"a-builtin"}}
}
