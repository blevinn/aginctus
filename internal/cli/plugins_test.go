package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/blevinn/aginctus/internal/plugins"
	"github.com/blevinn/aginctus/pkg/plugin"
)

type compiledFixture struct{}

func (compiledFixture) Descriptor() plugin.Descriptor {
	return plugin.Descriptor{APIVersion: plugin.APIVersion, Name: "fixture", Version: "v1", Capabilities: []plugin.Capability{{Name: "tools", Kind: plugin.MCP}}}
}
func (compiledFixture) ValidateConfiguration(context.Context, json.RawMessage) error {
	panic("inventory invoked configuration")
}

type failingInventoryWriter struct{}

func (failingInventoryWriter) Write([]byte) (int, error) { return 0, errors.New("sink unavailable") }

func TestCompiledPluginInventoryIsLocal(t *testing.T) {
	r, err := plugins.New(context.Background(), compiledFixture{})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	// Nil clients/loader deliberately make accidental host/config access fail.
	status := RunWithPlugins(context.Background(), []string{"--configuration-file", "/does/not/exist", "plugins", "inventory"}, &stdout, &stderr, nil, nil, r)
	if status != 0 || stderr.Len() != 0 {
		t.Fatalf("status=%d stderr=%s", status, &stderr)
	}
	var got plugins.CompiledInventory
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Plugins) != 1 || got.Plugins[0].Name != "fixture" {
		t.Fatalf("inventory: %+v", got)
	}
	if status := RunWithPlugins(context.Background(), []string{"plugins", "inventory"}, failingInventoryWriter{}, &stderr, nil, nil, r); status == 0 {
		t.Fatal("sink failure reported success")
	}
	stdout.Reset()
	stderr.Reset()
	if status := Run(context.Background(), []string{"plugins", "inventory"}, &stdout, &stderr, nil, nil); status != 0 {
		t.Fatalf("empty selection: %s", &stderr)
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil || len(got.Plugins) != 0 {
		t.Fatalf("empty selection: %s", &stdout)
	}
	for _, args := range [][]string{{"plugins", "inventory", "extra"}, {"plugins", "load"}} {
		stdout.Reset()
		stderr.Reset()
		if RunWithPlugins(context.Background(), args, &stdout, &stderr, nil, nil, r) == 0 {
			t.Fatalf("accepted %v", args)
		}
	}
}
