package client

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/blevinn/aginctus/pkg/plugin"
)

type fakeInventory []plugin.Descriptor

func (f fakeInventory) Inventory() []plugin.Descriptor { return []plugin.Descriptor(f) }

func TestPluginListTypedDetachedAndEmpty(t *testing.T) {
	empty, err := New(Options{}).Plugins.List(context.Background(), ListPluginsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(empty)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"apiVersion":"aginctus.dev/v1alpha1","kind":"PluginList","items":[]}` {
		t.Fatalf("empty document: %s", encoded)
	}
	source := fakeInventory{{
		APIVersion: plugin.APIVersion, Name: "first", Version: "1",
		Capabilities: []plugin.Capability{{Name: "agent", Kind: plugin.Agent}},
		Dependencies: []string{"another"},
	}}
	client := New(Options{PluginInventory: source})
	result, err := client.Plugins.List(context.Background(), ListPluginsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != "PluginList" || len(result.Items) != 1 ||
		result.Items[0].Capabilities[0].Kind != "agent" {
		t.Fatalf("unexpected projection: %#v", result)
	}
	result.Items[0].Dependencies[0] = "mutated"
	result.Items[0].Capabilities[0].Name = "mutated"
	if !reflect.DeepEqual(source[0].Dependencies, []string{"another"}) ||
		source[0].Capabilities[0].Name != "agent" {
		t.Fatal("public result aliases source descriptor")
	}
}

func TestPluginListCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := New(Options{}).Plugins.List(ctx, ListPluginsRequest{})
	if !errors.Is(err, context.Canceled) || len(result.Items) != 0 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
