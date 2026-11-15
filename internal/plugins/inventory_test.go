package plugins

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/pkg/plugin"
)

type inventoryPlugin struct {
	descriptor plugin.Descriptor
	calls      *int
}

func (p inventoryPlugin) Descriptor() plugin.Descriptor { *p.calls++; return p.descriptor }
func (inventoryPlugin) ValidateConfiguration(context.Context, json.RawMessage) error {
	panic("inventory must not validate runtime configuration")
}

func TestCompiledInventory(t *testing.T) {
	calls := 0
	a := plugin.Descriptor{APIVersion: plugin.APIVersion, Name: "z-base", Version: "fixture-1", Capabilities: []plugin.Capability{{Name: "z", Kind: plugin.MCP}, {Name: "a", Kind: plugin.ModelProvider}}}
	b := plugin.Descriptor{APIVersion: plugin.APIVersion, Name: "a-dependent", Version: "fixture-2", Capabilities: []plugin.Capability{{Name: "client", Kind: plugin.UI}}, Dependencies: []string{"z-base"}}
	r, err := New(context.Background(), inventoryPlugin{b, &calls}, inventoryPlugin{a, &calls})
	if err != nil {
		t.Fatal(err)
	}
	before := calls
	got := r.Compiled()
	if got.SchemaVersion != InventorySchemaVersion || len(got.Plugins) != 2 || got.Plugins[0].Name != "z-base" || got.Plugins[1].Dependencies[0] != "z-base" || got.Plugins[0].Capabilities[0].Name != "a" {
		t.Fatalf("inventory: %+v", got)
	}
	if got.Plugins[0].Version != a.Version || got.Plugins[0].APIVersion != plugin.APIVersion {
		t.Fatal("identity lost")
	}
	got.Plugins[0].Capabilities[0].Name = "changed"
	got.Plugins[1].Dependencies[0] = "changed"
	if r.Compiled().Plugins[0].Capabilities[0].Name != "a" || r.Compiled().Plugins[1].Dependencies[0] != "z-base" {
		t.Fatal("inventory aliases registry")
	}
	if calls != before {
		t.Fatal("inventory invoked implementation")
	}
	encoded, err := json.Marshal(r.Compiled())
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"schema_version":`, `"api_version":`, `"capabilities":`, `"dependencies":[]`} {
		if !strings.Contains(string(encoded), field) {
			t.Fatalf("missing %s: %s", field, encoded)
		}
	}
	var empty *Registry
	encoded, err = json.Marshal(empty.Compiled())
	if err != nil || string(encoded) != `{"schema_version":"aginctus.dev/compiled-plugins/v1alpha1","plugins":[]}` {
		t.Fatalf("empty inventory: %s, %v", encoded, err)
	}
}
