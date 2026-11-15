package plugins

import (
	"cmp"
	"slices"

	"github.com/blevinn/aginctus/pkg/plugin"
)

// InventorySchemaVersion identifies the experimental compiled-inventory wire
// schema, separately from plugin API and implementation versions.
const InventorySchemaVersion = "aginctus.dev/compiled-plugins/v1alpha1"

// CompiledInventory describes only registered contract implementations. It is
// not runtime configuration, an authorization grant or a provenance attestation.
type CompiledInventory struct {
	SchemaVersion string           `json:"schema_version"`
	Plugins       []CompiledPlugin `json:"plugins"`
}

type CompiledPlugin struct {
	Name         string               `json:"name"`
	Version      string               `json:"version"`
	APIVersion   string               `json:"api_version"`
	Capabilities []CompiledCapability `json:"capabilities"`
	Dependencies []string             `json:"dependencies"`
}

type CompiledCapability struct {
	Name string                `json:"name"`
	Kind plugin.CapabilityKind `json:"kind"`
}

// Compiled returns a detached JSON-ready view without invoking implementations.
// Plugin order is dependency-first. Capabilities/dependencies are lexical; empty
// lists serialize as [] rather than null. A nil registry is the empty selection.
func (r *Registry) Compiled() CompiledInventory {
	result := CompiledInventory{SchemaVersion: InventorySchemaVersion, Plugins: []CompiledPlugin{}}
	if r == nil {
		return result
	}
	for _, descriptor := range r.Inventory() {
		entry := CompiledPlugin{
			Name: descriptor.Name, Version: descriptor.Version, APIVersion: descriptor.APIVersion,
			Capabilities: []CompiledCapability{}, Dependencies: append([]string{}, descriptor.Dependencies...),
		}
		for _, capability := range descriptor.Capabilities {
			entry.Capabilities = append(entry.Capabilities, CompiledCapability(capability))
		}
		slices.SortFunc(entry.Capabilities, func(a, b CompiledCapability) int { return cmp.Compare(a.Name, b.Name) })
		slices.Sort(entry.Dependencies)
		result.Plugins = append(result.Plugins, entry)
	}
	return result
}
