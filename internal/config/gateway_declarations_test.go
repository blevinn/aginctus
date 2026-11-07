package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGatewayDeclarationSources(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.json")
	writeConfigFile(t, file, `{"gateway":{"providers":{"go":{"plugin":"opencode-go","config":{"models":{"old":{"source":"openai/gpt-6-luna"}}}}},"catalogs":{"base":{"models":{"old":{"provider":"go","name":"old"}}}}}}`)
	loader := &Loader{SystemPath: file}
	effective, err := loader.Load(Options{Overrides: []string{
		`gateway.providers.go={"plugin":"opencode-go","config":{"models":{"new":{"source":"openai/grok-4.7"}}}}`,
		`gateway.catalogs.base={"models":{"new":{"provider":"go","name":"new"}}}`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := effective.Get("gateway.providers.go.config.models.old"); ok {
		t.Fatal("provider definition inherited an old model across sources")
	}
	if _, ok := effective.Get("gateway.catalogs.base.models.old"); ok {
		t.Fatal("catalog definition inherited an old selection across sources")
	}
	for _, override := range []string{
		"gateway.providers.go.config.api_key=sentinel-private",
		`gateway.providers.go={"plugin":"opencode-go","config":{"mcps":{"docs":{"headers":{"Authorization":"sentinel-private"}}}}}`,
	} {
		if _, err := loader.Load(Options{Overrides: []string{override}}); err == nil || strings.Contains(err.Error(), "sentinel-private") {
			t.Fatalf("credential accepted or echoed: %v", err)
		}
	}
	loader.Environment = []string{`AGINCTUS_GATEWAY_PROVIDERS={"go":{"config":{"token":"sentinel-private"}}}`}
	if _, err := loader.Load(Options{}); err == nil || strings.Contains(err.Error(), "sentinel-private") {
		t.Fatalf("environment credential accepted or echoed: %v", err)
	}
	writeConfigFile(t, file, `{"gateway.providers.go.config.password":"sentinel-private"}`)
	if _, err := (&Loader{SystemPath: file}).Load(Options{}); err == nil || strings.Contains(err.Error(), "sentinel-private") {
		t.Fatalf("file credential accepted or echoed: %v", err)
	}
}
