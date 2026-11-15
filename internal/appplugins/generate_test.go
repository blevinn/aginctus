package appplugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/blevinn/aginctus/internal/pluginbuild"
)

func TestCheckedInComposition(t *testing.T) {
	raw, err := os.ReadFile("plugins.json")
	if err != nil {
		t.Fatal(err)
	}
	want, err := pluginbuild.Generate(raw)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("generated.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("run go generate ./internal/appplugins")
	}
	r, err := New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var manifest pluginbuild.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(r.Inventory()) != len(manifest.Plugins) {
		t.Fatal("inventory does not match the explicit selection")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
