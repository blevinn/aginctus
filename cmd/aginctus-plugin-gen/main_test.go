package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateAndCheck(t *testing.T) {
	dir := t.TempDir()
	manifest, out := filepath.Join(dir, "plugins.json"), filepath.Join(dir, "generated.go")
	if err := os.WriteFile(manifest, []byte(`{"api_version":"aginctus.dev/plugin-build/v1alpha1","package":"appplugins","plugins":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-manifest", manifest, "-out", out}
	if err := run(args, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := run(append(args, "-check"), io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(append(args, "-check"), io.Discard); err == nil {
		t.Fatal("stale source accepted")
	}
	current, _ := os.ReadFile(out)
	if string(current) != "stale" {
		t.Fatal("check mode wrote output")
	}
	if err := os.WriteFile(manifest, []byte(`{"api_version":"invalid"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(args, io.Discard); err == nil {
		t.Fatal("invalid manifest accepted")
	}
	current, _ = os.ReadFile(out)
	if string(current) != "stale" {
		t.Fatal("invalid manifest changed output")
	}
	if err := run(nil, io.Discard); err == nil {
		t.Fatal("missing arguments accepted")
	}
}
