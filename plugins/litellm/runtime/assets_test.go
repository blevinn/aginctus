package gatewayassets

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestBundle(t *testing.T) {
	sources := map[string]string{
		"config.yaml":                 "selective-go.yaml",
		"litellm_opencode_go_hook.py": "litellm_opencode_go_hook.py",
		"opencode_go_forwarding.py":   "opencode_go_forwarding.py",
	}
	files, manifest := Files(), Manifest()
	if len(files) != len(sources) || len(manifest) != len(sources) {
		t.Fatal("unexpected bundle entries")
	}
	for name, source := range sources {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if files[name] != string(data) {
			t.Fatalf("%s differs from canonical source", name)
		}
		if manifest[name] != fmt.Sprintf("%x", sha256.Sum256(data)) {
			t.Fatalf("%s digest mismatch", name)
		}
	}
	delete(files, "config.yaml")
	manifest["config.yaml"] = "mutated"
	if len(Files()) != 3 || Manifest()["config.yaml"] == "mutated" {
		t.Fatal("caller mutation affected bundle")
	}
}

// TestExportBundle is an explicit test-only exporter, usable independently of
// the gateway and without any host service or provider configuration.
func TestExportBundle(t *testing.T) {
	dir := os.Getenv("AGINCTUS_EXPORT_GO_BUNDLE")
	if dir == "" {
		t.Skip("set AGINCTUS_EXPORT_GO_BUNDLE to export")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range Files() {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.MarshalIndent(Manifest(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}
