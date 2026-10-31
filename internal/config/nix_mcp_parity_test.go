package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The NixOS module renders the MCP subtree from typed options. The Go
// schema is authoritative, so the rendered configuration must resolve to the
// effective policy the schema describes.
//
// The flake check supplies the rendered source. The snapshot keeps the same
// contract executable in plain Go tests without requiring Nix.
func TestNixOSMCPConfigParity(t *testing.T) {
	snapshotPath := "testdata/nixos-mcp.json"
	snapshot, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if generated := os.Getenv("AGINCTUS_NIXOS_MCP_FIXTURES"); generated != "" {
		produced := decodeJSONDocument(t, generated)
		if !reflect.DeepEqual(decodeJSONDocument(t, snapshotPath), produced) {
			t.Fatal("NixOS MCP configuration differs from testdata/nixos-mcp.json; regenerate and review the snapshot")
		}
	}

	path := filepath.Join(t.TempDir(), "config.json")
	writeConfigFile(t, path, string(snapshot))
	effective, err := (&Loader{SystemPath: path}).Load(Options{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	policy, err := effective.MCP()
	if err != nil {
		t.Fatalf("MCP() error = %v", err)
	}

	servers := policy.Servers()
	if len(servers) != 2 {
		t.Fatalf("servers = %d, want 2", len(servers))
	}
	docs := servers[0]
	if docs.Name != "docs" || docs.Transport != "streamable-http" {
		t.Fatalf("remote server = %+v", docs)
	}
	if docs.URL != "https://docs.example.com/mcp" {
		t.Fatalf("remote endpoint = %q", docs.URL)
	}
	if docs.CredentialRef != "docs-token" {
		t.Fatalf("credential reference = %q", docs.CredentialRef)
	}
	if !reflect.DeepEqual(docs.AllowedTools, []string{"fetch", "search"}) {
		t.Fatalf("server tool allowlist = %+v", docs.AllowedTools)
	}
	local := servers[1]
	if local.Name != "local" || local.Transport != "stdio" {
		t.Fatalf("local server = %+v", local)
	}
	if local.Command != "/usr/bin/mcp-local" {
		t.Fatalf("local command = %q", local.Command)
	}

	workloads := policy.Workloads()
	if len(workloads) != 1 || workloads[0].Workload != "dev" {
		t.Fatalf("workloads = %+v", workloads)
	}
	if workloads[0].Capability != "coding" || workloads[0].Protocol != "responses" {
		t.Fatalf("workload capability = %+v", workloads[0])
	}
	if !reflect.DeepEqual(workloads[0].Requires, []string{"mcp-tools"}) {
		t.Fatalf("required capabilities = %+v", workloads[0].Requires)
	}
	if len(workloads[0].Servers) != 1 || workloads[0].Servers[0].Server != "docs" {
		t.Fatalf("effective servers = %+v", workloads[0].Servers)
	}
	if !reflect.DeepEqual(workloads[0].Servers[0].Tools, []string{"search"}) {
		t.Fatalf("effective tools = %+v", workloads[0].Servers[0].Tools)
	}

	if !policy.Allows("dev", "docs", "search") {
		t.Fatal("the granted tool was not authorized")
	}
	if policy.Allows("dev", "docs", "fetch") {
		t.Fatal("a tool outside the workload grant was authorized")
	}
	if policy.Allows("dev", "local", "read") {
		t.Fatal("an ungranted server was authorized")
	}
	if !policy.Gateway().Enabled || !policy.Gateway().RequireAuthenticatedWorkload {
		t.Fatalf("gateway intent = %+v", policy.Gateway())
	}
	if policy.Gateway().UpstreamTimeoutSeconds != 30 {
		t.Fatalf("upstream timeout = %d", policy.Gateway().UpstreamTimeoutSeconds)
	}
}

func decodeJSONDocument(t *testing.T, path string) any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return decoded
}
