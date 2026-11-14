// Package plugins assembles explicitly selected plugin instances into scoped,
// validated declarations. It neither loads implementations nor executes targets.
// Compiled plugins remain trusted in-process code, not sandboxed extensions.
package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"sort"

	"github.com/blevinn/aginctus/pkg/orchestration"
	"github.com/blevinn/aginctus/pkg/plugin"
)

var localName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// Registry is an immutable registration snapshot. Only explicitly supplied
// implementations are available; there are no product imports or global builtins.
// Implementations must support their callers' concurrency and honor the public
// pure-callback contract; registry immutability does not sandbox plugin behavior.
type Registry struct {
	entries map[string]registered
	order   []string
}

type registered struct {
	implementation plugin.Plugin
	descriptor     plugin.Descriptor
}

func (r registered) Descriptor() plugin.Descriptor { return cloneDescriptor(r.descriptor) }

func (r registered) ValidateConfiguration(ctx context.Context, raw json.RawMessage) error {
	return r.implementation.ValidateConfiguration(ctx, raw)
}

type contributor struct {
	registered
	targets plugin.TargetContributor
}

func (c contributor) Targets(ctx context.Context, raw json.RawMessage) ([]orchestration.Target, error) {
	return c.targets.Targets(ctx, raw)
}

// New validates and snapshots descriptors, rejecting duplicates, incompatible
// APIs, missing compiled dependencies and cycles. Dependency-first order uses
// lexical tie-breaking, independent of registration order. Descriptor callbacks
// are pure by contract; cancellation cannot interrupt a noncooperative callback.
func New(ctx context.Context, implementations ...plugin.Plugin) (*Registry, error) {
	r := &Registry{entries: make(map[string]registered, len(implementations))}
	dependencies := make(map[string][]string, len(implementations))
	for i, implementation := range implementations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if nilImplementation(implementation) {
			return nil, fmt.Errorf("plugin %d has no implementation", i)
		}
		descriptor := cloneDescriptor(implementation.Descriptor())
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := descriptor.Validate(); err != nil {
			return nil, fmt.Errorf("register plugin %d: %w", i, err)
		}
		if _, exists := r.entries[descriptor.Name]; exists {
			return nil, fmt.Errorf("duplicate plugin identity")
		}
		r.entries[descriptor.Name] = registered{implementation, descriptor}
		dependencies[descriptor.Name] = descriptor.Dependencies
	}
	order, err := topologicalOrder(ctx, dependencies)
	if err != nil {
		return nil, fmt.Errorf("compiled plugin dependencies: %w", err)
	}
	r.order = order
	return r, nil
}

// Inventory returns detached descriptors in deterministic dependency-first order.
// It does not call implementations, create instances or prove build provenance.
func (r *Registry) Inventory() []plugin.Descriptor {
	result := make([]plugin.Descriptor, 0, len(r.order))
	for _, name := range r.order {
		result = append(result, cloneDescriptor(r.entries[name].descriptor))
	}
	return result
}

// Instance is an explicitly selected runtime declaration. Omitted instances do
// not imply deletion. Names and deployment IDs are logical scope, not Incus
// projects, credentials or controller authority. Configuration is nonsecret.
type Instance struct {
	Name          string
	Plugin        string
	Configuration json.RawMessage
}

// Assemble validates every selected instance/configuration before invoking any
// target contributor, then resolves the full target graph. Configuration and
// returned declarations are detached from caller/plugin-owned slices. No partial
// graph is returned on error. Discovery-only plugins are valid instances with no
// targets. Plugin dependencies do not auto-instantiate runtime dependencies.
func (r *Registry) Assemble(ctx context.Context, deployment string, instances []Instance) (Graph, error) {
	if err := ctx.Err(); err != nil {
		return Graph{}, err
	}
	if !localName.MatchString(deployment) {
		return Graph{}, fmt.Errorf("deployment must have an explicit valid identity")
	}
	selected := make([]Instance, len(instances))
	names := make(map[string]bool, len(instances))
	for i, instance := range instances {
		if !localName.MatchString(instance.Name) || names[instance.Name] {
			return Graph{}, fmt.Errorf("invalid or duplicate instance identity")
		}
		names[instance.Name] = true
		if _, exists := r.entries[instance.Plugin]; !exists {
			return Graph{}, fmt.Errorf("instance references an unregistered plugin")
		}
		selected[i] = instance
		selected[i].Configuration = slices.Clone(instance.Configuration)
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Name < selected[j].Name })
	for _, instance := range selected {
		if err := plugin.Validate(ctx, r.entries[instance.Plugin], instance.Configuration); err != nil {
			return Graph{}, fmt.Errorf("validate instance %s: %w", instance.Name, err)
		}
	}
	var targets []ScopedTarget
	for _, instance := range selected {
		entry := r.entries[instance.Plugin]
		implementation, contributes := entry.implementation.(plugin.TargetContributor)
		if !contributes {
			continue
		}
		contributions, err := plugin.Contribute(ctx, contributor{entry, implementation}, instance.Configuration)
		if err != nil {
			return Graph{}, fmt.Errorf("contribute instance %s: %w", instance.Name, err)
		}
		for _, definition := range contributions {
			targets = append(targets, ScopedTarget{
				Key:    TargetKey{Deployment: deployment, Instance: instance.Name, Target: definition.ID},
				Plugin: instance.Plugin, Definition: cloneTarget(definition),
			})
		}
	}
	return assembleGraph(ctx, deployment, targets)
}

func cloneDescriptor(d plugin.Descriptor) plugin.Descriptor {
	d.Capabilities = slices.Clone(d.Capabilities)
	d.Dependencies = slices.Clone(d.Dependencies)
	return d
}

func nilImplementation(implementation plugin.Plugin) bool {
	if implementation == nil {
		return true
	}
	v := reflect.ValueOf(implementation)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
