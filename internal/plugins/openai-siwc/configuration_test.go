package openaisiwc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/gatewaycontrol"
)

func TestConfigurationPreservesInheritedIdentityAndPrivateKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(dir, "workload-key")
	if err := os.WriteFile(key, []byte("sk-fixture-workload"), 0600); err != nil {
		t.Fatal(err)
	}
	plugins := gatewaycontrol.Builtins()
	plugins["openai-siwc"] = Plugin()
	store := &gatewaycontrol.Store{Directory: dir, Plugins: plugins}
	p := gatewaycontrol.Provider{Name: "personal", Plugin: "openai-siwc", Config: gatewaycontrol.Configuration{
		Models: map[string]gatewaycontrol.Model{"fixture-model": {Source: "fixture-model"}},
		Routes: map[string]gatewaycontrol.RouteConfiguration{"siwc-coding": {Catalog: "coding", WorkloadKeyFile: key}},
	}}
	if err := store.ApplyProvider(p); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyCatalog("parent", gatewaycontrol.Catalog{Models: map[string]gatewaycontrol.Asset{"original": {Provider: "personal", Name: "fixture-model"}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyCatalog("coding", gatewaycontrol.Catalog{Inherits: []gatewaycontrol.Import{{Catalog: "parent", Models: map[string]string{"original": "siwc-coding"}}}}); err != nil {
		t.Fatal(err)
	}
	plan, err := Configure(store)
	if err != nil {
		t.Fatal(err)
	}
	if route := plan.Routes["siwc-coding"]; route.Account != "personal" || route.Model != "fixture-model" || len(route.KeyHash) != 64 {
		t.Fatalf("lost source identity: %#v", route)
	}
	public, err := plan.RuntimeConfig()
	if err != nil || strings.Contains(public, "sk-fixture-workload") || strings.Contains(public, key) {
		t.Fatalf("unsafe public config: %v", err)
	}
	if err := os.Chmod(key, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Configure(store); err == nil {
		t.Fatal("accepted public workload credential")
	}
	p.Config.StateDir = "/private/manual-state"
	if err := plugins.Validate(p); err == nil {
		t.Fatal("accepted a second credential configuration plane")
	}
}
