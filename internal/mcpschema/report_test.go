package mcpschema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const reportFixture = `{
	"servers": {
		"docs": {
			"transport": "streamable-http",
			"url": "https://docs.example.com/mcp",
			"credentialRef": "docs-token",
			"allowedTools": ["fetch", "search"]
		},
		"local": {
			"transport": "stdio",
			"command": "/usr/bin/mcp-local",
			"args": ["--verbose"]
		}
	},
	"capabilities": {
		"coding": {"protocol": "responses", "requires": ["mcp-tools", "provider-hosted-tools"]}
	},
	"grants": {
		"dev": {"capability": "coding", "servers": {"docs": ["search"]}},
		"reader": {"capability": "coding", "servers": {}}
	},
	"gateway": {"enabled": true, "upstreamTimeoutSeconds": 45}
}`

func reportFixturePolicy(t *testing.T) Policy {
	t.Helper()
	config, err := Parse(decodeJSON(t, reportFixture))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	policy, err := config.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	return policy
}

func TestReportMatchesGoldenFixture(t *testing.T) {
	encoded, err := reportFixturePolicy(t).Report().JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	golden, err := os.ReadFile(filepath.Join("testdata", "policy-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != string(golden) {
		t.Fatalf("report does not match the golden fixture:\n%s", encoded)
	}
}

// The report is a stable contract for the gateway and OpenCode adapters that
// consume the policy. A change to these field names is a contract change.
func TestReportFieldNamesAreStable(t *testing.T) {
	encoded, err := reportFixturePolicy(t).Report().JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"servers", "workloads", "gateway"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("the report lost the %q section: %s", key, encoded)
		}
	}
	servers, ok := decoded["servers"].([]any)
	if !ok || len(servers) == 0 {
		t.Fatalf("the report lost the server list: %s", encoded)
	}
	for _, key := range []string{
		"name", "transport", "endpoint", "command", "hasCredentialReference", "allowedTools",
	} {
		if _, ok := servers[0].(map[string]any)[key]; !ok {
			t.Fatalf("the server report lost the %q field: %s", key, encoded)
		}
	}
	workloads, ok := decoded["workloads"].([]any)
	if !ok || len(workloads) == 0 {
		t.Fatalf("the report lost the workload list: %s", encoded)
	}
	for _, key := range []string{"workload", "capability", "protocol", "requires", "servers"} {
		if _, ok := workloads[0].(map[string]any)[key]; !ok {
			t.Fatalf("the workload report lost the %q field: %s", key, encoded)
		}
	}
	gateway, ok := decoded["gateway"].(map[string]any)
	if !ok {
		t.Fatalf("the report lost the gateway section: %s", encoded)
	}
	for _, key := range []string{
		"enabled", "requireAuthenticatedWorkload", "upstreamTimeoutSeconds",
	} {
		if _, ok := gateway[key]; !ok {
			t.Fatalf("the gateway report lost the %q field: %s", key, encoded)
		}
	}
}

// The report is redacted. It never discloses a credential reference, and it
// never discloses credential material that a rejected declaration carried.
func TestReportNeverDisclosesCredentialMaterial(t *testing.T) {
	const sentinel = "sentinel-credential-value"
	policy := reportFixturePolicy(t)
	encoded, err := policy.Report().JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	if strings.Contains(string(encoded), sentinel) {
		t.Fatal("the report disclosed credential material")
	}
	if strings.Contains(string(encoded), "docs-token") {
		t.Fatal("the report disclosed the credential reference")
	}
	if !strings.Contains(string(encoded), "hasCredentialReference\": true") {
		t.Fatalf("the report does not state that a credential reference exists: %s", encoded)
	}
}

func TestReportIsDeterministic(t *testing.T) {
	policy := reportFixturePolicy(t)
	first, err := policy.Report().JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	for iteration := 0; iteration < 25; iteration++ {
		again, err := policy.Report().JSON()
		if err != nil {
			t.Fatalf("JSON() error = %v", err)
		}
		if string(again) != string(first) {
			t.Fatalf("the report changed between calls:\n%s\n%s", first, again)
		}
	}
}

func TestReportIsImmutableToCallers(t *testing.T) {
	policy := reportFixturePolicy(t)
	report := policy.Report()
	report.Servers[0].Name = "tampered"
	report.Servers[0].AllowedTools[0] = "tampered"
	report.Workloads[0].Servers[0].Tools[0] = "tampered"
	report.Workloads[0].Requires[0] = "tampered"

	again := policy.Report()
	if again.Servers[0].Name != "docs" || again.Servers[0].AllowedTools[0] != "fetch" {
		t.Fatalf("the server report was mutated through the returned value: %+v", again.Servers[0])
	}
	if again.Workloads[0].Servers[0].Tools[0] != "search" {
		t.Fatalf("the workload report was mutated through the returned value: %+v", again.Workloads[0])
	}
	if again.Workloads[0].Requires[0] != CapabilityMCPTools {
		t.Fatalf("the required capabilities were mutated through the returned value: %+v", again.Workloads[0])
	}
}

// An absent server-level restriction and an empty server-level restriction
// are different states, and the report keeps them apart.
func TestReportKeepsUnrestrictedAndEmptyAllowlistsApart(t *testing.T) {
	config, err := Parse(decodeJSON(t, `{
		"servers": {
			"open": {"transport": "streamable-http", "url": "https://open.example.com/mcp"},
			"closed": {"transport": "streamable-http", "url": "https://closed.example.com/mcp", "allowedTools": []}
		},
		"gateway": {"enabled": true}
	}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	policy, err := config.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	report := policy.Report()
	byName := map[string]ServerReport{}
	for _, server := range report.Servers {
		byName[server.Name] = server
	}
	if byName["open"].AllowedTools != nil {
		t.Fatalf("an unrestricted server reported an allowlist: %+v", byName["open"])
	}
	if byName["closed"].AllowedTools == nil {
		t.Fatalf("an empty allowlist was reported as unrestricted: %+v", byName["closed"])
	}
}
