package gatewaycontrol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/blevinn/aginctus/internal/modelregistry"
	"github.com/blevinn/aginctus/internal/providerregistry"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Directory: filepath.Join(t.TempDir(), "gateway"), Plugins: Builtins()}
}
func providerFixture() Provider {
	return Provider{Name: "personal", Plugin: "chatgpt", Config: Configuration{Models: map[string]Model{"coding": {Source: "chatgpt/fixture-model"}}}}
}

func TestProviderInstancesAndCatalogSelection(t *testing.T) {
	s := testStore(t)
	p := providerFixture()
	if err := s.ApplyProvider(p); err != nil {
		t.Fatal(err)
	}
	mcp := json.RawMessage(`{"transport":"streamable-http","url":"https://tools.example.invalid/mcp","credentialRef":"search-session","allowedTools":["search"]}`)
	if err := s.ApplyMCP("search", mcp); err != nil {
		t.Fatal(err)
	}
	base := Catalog{Models: map[string]Asset{"coding": {Provider: p.Name, Name: "coding"}}, MCPs: map[string]Asset{"search": {Local: true, Name: "search"}}}
	if err := s.ApplyCatalog("base", base); err != nil {
		t.Fatal(err)
	}
	child := Catalog{Inherits: []Import{{Catalog: "base", Models: map[string]string{"coding": "dev.coding"}, MCPs: map[string]string{"search": "tools.search"}}}}
	if err := s.ApplyCatalog("work", child); err != nil {
		t.Fatal(err)
	}
	assets, err := s.Assets()
	if err != nil {
		t.Fatal(err)
	}
	providers, err := s.Providers()
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := Resolve("work", providers, assets)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Models["dev.coding"].Provider != "personal" || !resolved.MCPs["tools.search"].Local {
		t.Fatal("lost asset identity while renaming")
	}
	base.Models["extra"] = Asset{Provider: p.Name, Name: "coding"}
	if err := s.ApplyCatalog("base", base); err != nil {
		t.Fatal(err)
	}
	assets, err = s.Assets()
	if err != nil {
		t.Fatal(err)
	}
	resolved, err = Resolve("work", providers, assets)
	if err != nil || len(resolved.Models) != 1 {
		t.Fatal("parent addition widened child selection")
	}
	// Removing a referenced model must preserve the last valid registry.
	p.Config.Models = nil
	if err := s.ApplyProvider(p); err == nil {
		t.Fatal("orphaned catalog model reference")
	}
	providers, err = s.Providers()
	if err != nil || providers["personal"].Config.Models["coding"].Source == "" {
		t.Fatal("failed update changed provider")
	}
}

func TestCatalogRejectsCyclesCollisionsAndUnknownAssets(t *testing.T) {
	s := testStore(t)
	p := providerFixture()
	if err := s.ApplyProvider(p); err != nil {
		t.Fatal(err)
	}
	base := Catalog{Models: map[string]Asset{"coding": {Provider: p.Name, Name: "coding"}}}
	if err := s.ApplyCatalog("base", base); err != nil {
		t.Fatal(err)
	}
	for _, catalog := range []Catalog{
		{Inherits: []Import{{Catalog: "missing"}}},
		{Inherits: []Import{{Catalog: "cycle"}}},
		{Models: map[string]Asset{"x": {Provider: "missing", Name: "coding"}}},
		{MCPs: map[string]Asset{"x": {Local: true, Name: "missing"}}},
		{Inherits: []Import{{Catalog: "base", Models: map[string]string{"missing": "x"}}}},
		{Inherits: []Import{{Catalog: "base", Models: map[string]string{"coding": "x"}}}, Models: map[string]Asset{"x": {Provider: p.Name, Name: "coding"}}},
	} {
		if err := s.ApplyCatalog("cycle", catalog); err == nil {
			t.Fatalf("accepted invalid catalog: %+v", catalog)
		}
	}
	assets, err := s.Assets()
	if err != nil || len(assets.Catalogs) != 1 {
		t.Fatal("failed catalog update changed registry")
	}
}

func TestDeclarationsRejectSecretsAndUnknownFields(t *testing.T) {
	s := testStore(t)
	for _, data := range []string{
		`{"transport":"streamable-http","url":"https://tools.example.invalid/mcp","token":"secret-canary"}`,
		`{"transport":"stdio","command":"/bin/tool","env":{"TOKEN":"secret-canary"}}`,
		`{"transport":"stdio","command":"/bin/tool","args":["--token=secret-canary"]}`,
		`{"transport":"streamable-http","url":"https://u:secret-canary@tools.example.invalid/mcp"}`,
	} {
		if err := s.ApplyMCP("tools", json.RawMessage(data)); err == nil {
			t.Fatal("accepted credential material")
		}
	}
	for _, data := range []string{`null`, `[]`, `{"api_key":"secret-canary"}`, `{} {}`} {
		var c Configuration
		if err := Decode([]byte(data), &c); err == nil {
			t.Fatal("accepted invalid provider config")
		}
	}
	if _, err := os.Stat(s.Directory); !os.IsNotExist(err) {
		t.Fatal("invalid declarations created state")
	}
}

func TestLegacyRegistrationsRemainVisibleAndReadDoesNotInitialize(t *testing.T) {
	s := testStore(t)
	if _, err := s.Providers(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Assets(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Directory); !os.IsNotExist(err) {
		t.Fatal("read initialized state")
	}
	t.Setenv("XDG_STATE_HOME", filepath.Dir(s.Directory))
	path, err := providerregistry.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	s.Directory = filepath.Dir(path)
	if _, err := providerregistry.Register(path, providerregistry.Provider{Type: "chatgpt", Alias: "personal"}); err != nil {
		t.Fatal(err)
	}
	if _, err := modelregistry.Register(filepath.Join(s.Directory, "models.json"), modelregistry.Model{Name: "coding", Provider: "personal", Source: "chatgpt/fixture-model"}); err != nil {
		t.Fatal(err)
	}
	providers, err := s.Providers()
	if err != nil || providers["personal"].Config.Models["coding"].Source != "chatgpt/fixture-model" {
		t.Fatalf("legacy model invisible: %v", err)
	}
}

func TestProviderIdentityCannotBeRetargeted(t *testing.T) {
	s := testStore(t)
	p := providerFixture()
	if err := s.ApplyProvider(p); err != nil {
		t.Fatal(err)
	}
	p.Plugin = "opencode-go"
	p.Config.Models = nil
	if err := s.ApplyProvider(p); err == nil {
		t.Fatal("retargeted provider plugin")
	}
}
