package plugins_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/blevinn/aginctus/internal/plugins"
	"github.com/blevinn/aginctus/pkg/orchestration"
	"github.com/blevinn/aginctus/pkg/plugin"
)

// These implementations use only public contract types. The host below uses
// the internal registry, not concrete in-tree product integrations or clients.
type discovery struct {
	descriptor plugin.Descriptor
	validate   func(context.Context, json.RawMessage) error
	onDescribe func()
}

func newDiscovery(name string, dependencies ...string) *discovery {
	return &discovery{descriptor: plugin.Descriptor{
		APIVersion: plugin.APIVersion, Name: name, Version: "fixture-1", Dependencies: dependencies,
		Capabilities: []plugin.Capability{{Name: "work", Kind: plugin.Agent}},
	}}
}

func (p *discovery) Descriptor() plugin.Descriptor {
	if p.onDescribe != nil {
		p.onDescribe()
	}
	return p.descriptor
}

func (p *discovery) ValidateConfiguration(ctx context.Context, raw json.RawMessage) error {
	if p.validate != nil {
		return p.validate(ctx, raw)
	}
	return ctx.Err()
}

type contribution struct {
	*discovery
	targets func(context.Context, json.RawMessage) ([]orchestration.Target, error)
}

func (p contribution) Targets(ctx context.Context, raw json.RawMessage) ([]orchestration.Target, error) {
	return p.targets(ctx, raw)
}

func instance(name, implementation string) plugins.Instance {
	return plugins.Instance{Name: name, Plugin: implementation, Configuration: json.RawMessage(`{}`)}
}

func registry(t *testing.T, implementations ...plugin.Plugin) *plugins.Registry {
	t.Helper()
	r, err := plugins.New(context.Background(), implementations...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRegistryRejectsInvalidCompiledSets(t *testing.T) {
	unsupported := newDiscovery("future")
	unsupported.descriptor.APIVersion = "future"
	var typedNil *discovery
	for _, tt := range []struct {
		name            string
		implementations []plugin.Plugin
		cause           error
	}{
		{"nil", []plugin.Plugin{nil}, nil},
		{"typed nil", []plugin.Plugin{typedNil}, nil},
		{"duplicate", []plugin.Plugin{newDiscovery("same"), newDiscovery("same")}, nil},
		{"incompatible", []plugin.Plugin{unsupported}, plugin.ErrUnsupportedAPIVersion},
		{"missing dependency", []plugin.Plugin{newDiscovery("dependent", "missing")}, nil},
		{"cycle", []plugin.Plugin{newDiscovery("first", "second"), newDiscovery("second", "first")}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, err := plugins.New(context.Background(), tt.implementations...)
			if err == nil || r != nil {
				t.Fatalf("invalid compiled set registered: %v, %v", r, err)
			}
			if tt.cause != nil && !errors.Is(err, tt.cause) {
				t.Fatalf("error = %v, want cause %v", err, tt.cause)
			}
		})
	}
}

func TestInventoryIsOrderedAndDetached(t *testing.T) {
	base, dependent, other := newDiscovery("base"), newDiscovery("alpha", "base"), newDiscovery("zeta")
	r := registry(t, other, dependent, base)
	want := registry(t, base, other, dependent).Inventory()
	got := r.Inventory()
	if !reflect.DeepEqual(got, want) || got[0].Name != "base" || got[1].Name != "alpha" || got[2].Name != "zeta" {
		t.Fatalf("registration order changed dependency-first inventory: %+v", got)
	}
	base.descriptor.Capabilities[0].Name = "changed-by-plugin"
	dependent.descriptor.Dependencies[0] = "changed-by-plugin"
	got[0].Capabilities[0].Name = "changed-by-caller"
	got[1].Dependencies[0] = "changed-by-caller"
	if !reflect.DeepEqual(r.Inventory(), want) {
		t.Fatal("registered metadata aliases plugin or caller memory")
	}
}

func TestSelectionAndConfigurationValidationPrecedeContributions(t *testing.T) {
	called := 0
	want := errors.New("invalid candidate")
	p := contribution{discovery: newDiscovery("example"), targets: func(context.Context, json.RawMessage) ([]orchestration.Target, error) {
		called++
		return []orchestration.Target{{ID: "target", Capability: "work"}}, nil
	}}
	p.validate = func(_ context.Context, raw json.RawMessage) error {
		if string(raw) == `{"invalid":true}` {
			return want
		}
		return nil
	}
	r := registry(t, p)
	for _, tt := range []struct {
		name       string
		deployment string
		instances  []plugins.Instance
		cause      error
	}{
		{"no deployment", "", []plugins.Instance{instance("first", "example")}, nil},
		{"invalid deployment", "../host", nil, nil},
		{"unknown plugin", "lab", []plugins.Instance{instance("first", "missing")}, nil},
		{"duplicate instance", "lab", []plugins.Instance{instance("first", "example"), instance("first", "example")}, nil},
		{"invalid instance", "lab", []plugins.Instance{instance("bad/name", "example")}, nil},
		{"later invalid configuration", "lab", []plugins.Instance{
			instance("first", "example"), {Name: "second", Plugin: "example", Configuration: json.RawMessage(`{"invalid":true}`)},
		}, want},
		{"bad JSON envelope", "lab", []plugins.Instance{{Name: "first", Plugin: "example", Configuration: json.RawMessage(`null`)}}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			graph, err := r.Assemble(context.Background(), tt.deployment, tt.instances)
			if err == nil || !reflect.DeepEqual(graph, plugins.Graph{}) || called != 0 {
				t.Fatalf("invalid selection contributed targets: %+v, %v, calls=%d", graph, err, called)
			}
			if tt.cause != nil && !errors.Is(err, tt.cause) {
				t.Fatalf("error = %v, want cause %v", err, tt.cause)
			}
		})
	}
}

func TestDiscoveryAndOmittedInstancesDoNotCreateTargets(t *testing.T) {
	p := newDiscovery("catalog")
	validated := 0
	p.validate = func(context.Context, json.RawMessage) error { validated++; return nil }
	r := registry(t, p, newDiscovery("dependent", "catalog"))
	graph, err := r.Assemble(context.Background(), "lab", []plugins.Instance{instance("only", "catalog")})
	if err != nil || len(graph.Targets) != 0 || validated != 1 {
		t.Fatalf("discovery-only instance: %+v, %v, validations=%d", graph, err, validated)
	}
	graph, err = r.Assemble(context.Background(), "lab", nil)
	if err != nil || graph.Deployment != "lab" || len(graph.Targets) != 0 || validated != 1 {
		t.Fatalf("omission enabled an instance: %+v, %v", graph, err)
	}
	if _, err := registry(t).Assemble(context.Background(), "lab", nil); err != nil {
		t.Fatalf("empty registry/selection rejected: %v", err)
	}
}

func TestCancellationAndCallbackErrorsReturnNoGraph(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := newDiscovery("example")
	p.onDescribe = func() { t.Fatal("called descriptor after cancellation") }
	if _, err := plugins.New(ctx, p); !errors.Is(err, context.Canceled) {
		t.Fatalf("registration error = %v, want cancellation", err)
	}
	p.onDescribe = nil
	want := errors.New("contribution unavailable")
	contributes := contribution{discovery: p, targets: func(context.Context, json.RawMessage) ([]orchestration.Target, error) {
		return nil, want
	}}
	r := registry(t, contributes)
	if graph, err := r.Assemble(context.Background(), "lab", []plugins.Instance{instance("one", "example")}); !errors.Is(err, want) || !reflect.DeepEqual(graph, plugins.Graph{}) {
		t.Fatalf("callback error lost: %+v, %v", graph, err)
	}
	if _, err := r.Assemble(ctx, "lab", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("assembly error = %v, want cancellation", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	p.validate = func(context.Context, json.RawMessage) error { cancel(); return nil }
	contributes.targets = func(context.Context, json.RawMessage) ([]orchestration.Target, error) {
		t.Fatal("contributed after validation canceled")
		return nil, nil
	}
	r = registry(t, contributes)
	if graph, err := r.Assemble(ctx, "lab", []plugins.Instance{instance("one", "example")}); !errors.Is(err, context.Canceled) || !reflect.DeepEqual(graph, plugins.Graph{}) {
		t.Fatalf("canceled configuration established success: %+v, %v", graph, err)
	}
	p.validate = nil
	ctx, cancelTargets := context.WithCancel(context.Background())
	defer cancelTargets()
	contributes.targets = func(context.Context, json.RawMessage) ([]orchestration.Target, error) {
		cancelTargets()
		return []orchestration.Target{{ID: "target", Capability: "work"}}, nil
	}
	r = registry(t, contributes)
	if graph, err := r.Assemble(ctx, "lab", []plugins.Instance{instance("one", "example")}); !errors.Is(err, context.Canceled) || !reflect.DeepEqual(graph, plugins.Graph{}) {
		t.Fatalf("canceled contribution established success: %+v, %v", graph, err)
	}
}
