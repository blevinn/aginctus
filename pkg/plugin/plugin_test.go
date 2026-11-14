package plugin_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/pkg/plugin"
)

func descriptor() plugin.Descriptor {
	return plugin.Descriptor{
		APIVersion: plugin.APIVersion,
		Name:       "example",
		Version:    "v0.1.0+fixture",
		Capabilities: []plugin.Capability{
			{Name: "agent", Kind: plugin.Agent},
			{Name: "gateway", Kind: plugin.Gateway},
			{Name: "client", Kind: plugin.UI},
			{Name: "tools", Kind: plugin.MCP},
			{Name: "models", Kind: plugin.ModelProvider},
			{Name: "other-models", Kind: plugin.ModelProvider},
		},
	}
}

func TestDescriptorComposition(t *testing.T) {
	d := descriptor()
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if d.Capabilities[0].Kind != plugin.Agent || d.Capabilities[5].Name != "other-models" {
		t.Fatal("validation changed capability order")
	}
	// A provider-only plugin does not need an agent or container capability.
	d.Capabilities = d.Capabilities[4:]
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDescriptorRejectsAmbiguousOrIncompatibleDiscovery(t *testing.T) {
	tests := []struct {
		name   string
		change func(*plugin.Descriptor)
		cause  error
	}{
		{"missing API", func(d *plugin.Descriptor) { d.APIVersion = "" }, plugin.ErrUnsupportedAPIVersion},
		{"future API", func(d *plugin.Descriptor) { d.APIVersion += "-future" }, plugin.ErrUnsupportedAPIVersion},
		{"missing name", func(d *plugin.Descriptor) { d.Name = "" }, nil},
		{"path as name", func(d *plugin.Descriptor) { d.Name = "../example" }, nil},
		{"long name", func(d *plugin.Descriptor) { d.Name = strings.Repeat("a", 129) }, nil},
		{"missing version", func(d *plugin.Descriptor) { d.Version = "" }, nil},
		{"version control character", func(d *plugin.Descriptor) { d.Version = "v1\x1b[0m" }, nil},
		{"version whitespace", func(d *plugin.Descriptor) { d.Version = "v1 beta" }, nil},
		{"no capability", func(d *plugin.Descriptor) { d.Capabilities = nil }, nil},
		{"invalid capability name", func(d *plugin.Descriptor) { d.Capabilities[0].Name = "/agent" }, nil},
		{"unknown kind", func(d *plugin.Descriptor) { d.Capabilities[0].Kind = "future" }, nil},
		{"duplicate name across kinds", func(d *plugin.Descriptor) { d.Capabilities[1].Name = "agent" }, nil},
		{"invalid dependency", func(d *plugin.Descriptor) { d.Dependencies = []string{"../other"} }, nil},
		{"self dependency", func(d *plugin.Descriptor) { d.Dependencies = []string{d.Name} }, nil},
		{"duplicate dependency", func(d *plugin.Descriptor) { d.Dependencies = []string{"other", "other"} }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := descriptor()
			tt.change(&d)
			err := d.Validate()
			if err == nil {
				t.Fatal("invalid descriptor accepted")
			}
			if tt.cause != nil && !errors.Is(err, tt.cause) {
				t.Fatalf("error = %v, want cause %v", err, tt.cause)
			}
		})
	}
}

type implementation struct {
	descriptor plugin.Descriptor
	validate   func(context.Context, json.RawMessage) error
}

func (p implementation) Descriptor() plugin.Descriptor { return p.descriptor }

func (p implementation) ValidateConfiguration(ctx context.Context, raw json.RawMessage) error {
	return p.validate(ctx, raw)
}

func TestValidateRejectsBeforeCallingPlugin(t *testing.T) {
	for _, raw := range []string{"", "null", "[]", `"value"`, "{} {}", `{"bad":`, "42"} {
		t.Run(raw, func(t *testing.T) {
			p := implementation{descriptor: descriptor(), validate: func(context.Context, json.RawMessage) error {
				t.Fatal("called plugin with invalid configuration envelope")
				return nil
			}}
			if err := plugin.Validate(context.Background(), p, json.RawMessage(raw)); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	p := implementation{descriptor: descriptor(), validate: func(context.Context, json.RawMessage) error {
		t.Fatal("called incompatible plugin")
		return nil
	}}
	p.descriptor.APIVersion = "future"
	if err := plugin.Validate(context.Background(), p, json.RawMessage(`{}`)); !errors.Is(err, plugin.ErrUnsupportedAPIVersion) {
		t.Fatalf("error = %v, want unsupported API", err)
	}
	if err := plugin.Validate(context.Background(), nil, json.RawMessage(`{}`)); err == nil {
		t.Fatal("nil implementation accepted")
	}
}

func TestValidatePassesConfigurationAndRetainsErrors(t *testing.T) {
	want := errors.New("invalid combination")
	raw := json.RawMessage(`{"model":"fixture"}`)
	p := implementation{descriptor: descriptor(), validate: func(_ context.Context, got json.RawMessage) error {
		if string(got) != string(raw) {
			t.Fatalf("configuration = %s, want %s", got, raw)
		}
		return want
	}}
	if err := plugin.Validate(context.Background(), p, raw); !errors.Is(err, want) {
		t.Fatalf("error = %v, want plugin error cause", err)
	}
	p.validate = func(context.Context, json.RawMessage) error { return nil }
	if err := plugin.Validate(context.Background(), p, raw); err != nil {
		t.Fatal(err)
	}
}

func TestValidateCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := implementation{descriptor: descriptor(), validate: func(context.Context, json.RawMessage) error {
		t.Fatal("called plugin after cancellation")
		return nil
	}}
	if err := plugin.Validate(ctx, p, json.RawMessage(`{}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	p.validate = func(context.Context, json.RawMessage) error {
		cancel()
		return nil // A callback that ignores cancellation cannot establish success.
	}
	if err := plugin.Validate(ctx, p, json.RawMessage(`{}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation after callback", err)
	}
}
