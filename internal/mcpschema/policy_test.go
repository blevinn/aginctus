package mcpschema

import (
	"sync"
	"strings"
	"testing"
)

func resolve(t *testing.T, document string) Policy {
	t.Helper()
	config, err := Parse(decodeJSON(t, document))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	policy, err := config.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	return policy
}

func resolveError(t *testing.T, document string) error {
	t.Helper()
	config, err := Parse(decodeJSON(t, document))
	if err != nil {
		return err
	}
	if _, err := config.Resolve(); err != nil {
		return err
	}
	return nil
}

const remoteServerConfig = `{
	"servers": {
		"docs": {
			"transport": "streamable-http",
			"url": "https://docs.example.com/mcp",
			"credentialRef": "docs-token",
			"allowedTools": ["fetch", "search"]
		}
	},
	"capabilities": {"coding": {"protocol": "responses", "requires": ["mcp-tools"]}},
	"grants": {"dev": {"capability": "coding", "servers": {"docs": ["search"]}}},
	"gateway": {"enabled": true}
}`

func TestResolveWithoutGrantsGrantsNoAccess(t *testing.T) {
	policy := resolve(t, `{
		"servers": {"docs": {"transport": "streamable-http", "url": "https://docs.example.com/mcp"}},
		"gateway": {"enabled": true}
	}`)
	if len(policy.Workloads()) != 0 {
		t.Fatalf("workloads = %+v, want none", policy.Workloads())
	}
	if policy.Allows("dev", "docs", "search") {
		t.Fatal("a workload without grants was authorized")
	}
	if len(policy.Servers()) != 1 {
		t.Fatalf("servers = %d, want 1", len(policy.Servers()))
	}
}

func TestResolveAppliesServerRestrictionAndWorkloadGrant(t *testing.T) {
	policy := resolve(t, remoteServerConfig)
	if !policy.Allows("dev", "docs", "search") {
		t.Fatal("the granted tool was not authorized")
	}
	if policy.Allows("dev", "docs", "fetch") {
		t.Fatal("a tool outside the workload grant was authorized")
	}
	if policy.Allows("other", "docs", "search") {
		t.Fatal("an ungranted workload was authorized")
	}
	if policy.Allows("dev", "unknown", "search") {
		t.Fatal("an unknown server was authorized")
	}
	workload, ok := policy.Workload("dev")
	if !ok {
		t.Fatal("the granted workload is missing from the policy")
	}
	if len(workload.Servers) != 1 || workload.Servers[0].Server != "docs" {
		t.Fatalf("effective servers = %+v", workload.Servers)
	}
	if len(workload.Servers[0].Tools) != 1 || workload.Servers[0].Tools[0] != "search" {
		t.Fatalf("effective tools = %+v", workload.Servers[0].Tools)
	}
	if workload.Protocol != ProtocolResponses {
		t.Fatalf("workload protocol = %q", workload.Protocol)
	}
}

func TestResolveRejectsToolGrantOutsideServerAllowlist(t *testing.T) {
	err := resolveError(t, `{
		"servers": {
			"docs": {
				"transport": "streamable-http",
				"url": "https://docs.example.com/mcp",
				"allowedTools": ["search"]
			}
		},
		"capabilities": {"coding": {"requires": ["mcp-tools"]}},
		"grants": {"dev": {"capability": "coding", "servers": {"docs": ["fetch"]}}},
		"gateway": {"enabled": true}
	}`)
	if err == nil {
		t.Fatal("Resolve() accepted a tool grant outside the server allowlist")
	}
	if !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveRejectsUnknownGrantTargets(t *testing.T) {
	cases := map[string]string{
		"unknownServer": `{
			"servers": {"docs": {"transport": "streamable-http", "url": "https://docs.example.com/mcp"}},
			"capabilities": {"coding": {"requires": ["mcp-tools"]}},
			"grants": {"dev": {"capability": "coding", "servers": {"unknown": ["search"]}}},
			"gateway": {"enabled": true}
		}`,
		"unknownCapability": `{
			"servers": {"docs": {"transport": "streamable-http", "url": "https://docs.example.com/mcp"}},
			"grants": {"dev": {"capability": "missing", "servers": {"docs": ["search"]}}},
			"gateway": {"enabled": true}
		}`,
		"missingCapability": `{
			"servers": {"docs": {"transport": "streamable-http", "url": "https://docs.example.com/mcp"}},
			"grants": {"dev": {"servers": {"docs": ["search"]}}},
			"gateway": {"enabled": true}
		}`,
		"capabilityWithoutTools": `{
			"servers": {"docs": {"transport": "streamable-http", "url": "https://docs.example.com/mcp"}},
			"capabilities": {"coding": {"requires": ["provider-hosted-tools"]}},
			"grants": {"dev": {"capability": "coding", "servers": {"docs": ["search"]}}},
			"gateway": {"enabled": true}
		}`,
		"duplicateToolGrant": `{
			"servers": {"docs": {"transport": "streamable-http", "url": "https://docs.example.com/mcp"}},
			"capabilities": {"coding": {"requires": ["mcp-tools"]}},
			"grants": {"dev": {"capability": "coding", "servers": {"docs": ["search", "search"]}}},
			"gateway": {"enabled": true}
		}`,
	}
	for name, document := range cases {
		t.Run(name, func(t *testing.T) {
			if err := resolveError(t, document); err == nil {
				t.Fatal("Resolve() accepted an invalid grant")
			}
		})
	}
}

func TestResolveRejectsUnsupportedCapabilities(t *testing.T) {
	for _, capability := range []string{"resources", "prompts", "sampling", "elicitation", "roots"} {
		t.Run(capability, func(t *testing.T) {
			document := `{
				"capabilities": {"coding": {"requires": ["` + capability + `"]}},
				"grants": {"dev": {"capability": "coding"}}
			}`
			err := resolveError(t, document)
			if err == nil {
				t.Fatal("Resolve() accepted an unsupported capability")
			}
			if !strings.Contains(err.Error(), capability) {
				t.Fatalf("error does not name the unsupported capability: %v", err)
			}
		})
	}
}

func TestProviderHostedToolsGrantNoMCPAccess(t *testing.T) {
	policy := resolve(t, `{
		"capabilities": {"coding": {"requires": ["provider-hosted-tools"]}},
		"grants": {"dev": {"capability": "coding"}}
	}`)
	workload, ok := policy.Workload("dev")
	if !ok {
		t.Fatal("the declared workload is missing from the policy")
	}
	if len(workload.Requires) != 1 || workload.Requires[0] != CapabilityProviderHostedTools {
		t.Fatalf("required capabilities = %+v", workload.Requires)
	}
	if len(workload.Servers) != 0 {
		t.Fatalf("provider-hosted tools produced MCP server grants: %+v", workload.Servers)
	}
	if policy.Allows("dev", "docs", "search") {
		t.Fatal("provider-hosted tools authorized an MCP tool")
	}
}

func TestResolveRejectsContradictoryGatewayIntent(t *testing.T) {
	cases := map[string]string{
		"enabledWithoutRemoteServer": `{
			"gateway": {"enabled": true}
		}`,
		"remoteServerWithoutGateway": `{
			"servers": {"docs": {"transport": "streamable-http", "url": "https://docs.example.com/mcp"}}
		}`,
		"unauthenticatedWorkloads": `{
			"servers": {"docs": {"transport": "streamable-http", "url": "https://docs.example.com/mcp"}},
			"gateway": {"enabled": true, "requireAuthenticatedWorkload": false}
		}`,
		"timeoutTooLow": `{
			"gateway": {"upstreamTimeoutSeconds": 0}
		}`,
		"timeoutTooHigh": `{
			"gateway": {"upstreamTimeoutSeconds": 301}
		}`,
	}
	for name, document := range cases {
		t.Run(name, func(t *testing.T) {
			if err := resolveError(t, document); err == nil {
				t.Fatal("Resolve() accepted a contradictory configuration")
			}
		})
	}
}

func TestStdioServerNeedsNoGatewayIntent(t *testing.T) {
	policy := resolve(t, `{
		"servers": {"local": {"transport": "stdio", "command": "/usr/bin/mcp-local"}},
		"capabilities": {"coding": {"requires": ["mcp-tools"]}},
		"grants": {"dev": {"capability": "coding", "servers": {"local": ["read"]}}}
	}`)
	workload, _ := policy.Workload("dev")
	if len(workload.Servers) != 1 || workload.Servers[0].Transport != TransportStdio {
		t.Fatalf("effective servers = %+v", workload.Servers)
	}
	if policy.Gateway().Enabled {
		t.Fatal("a workload-local stdio server enabled the central gateway intent")
	}
}

func TestEmptyToolGrantAuthorizesNothing(t *testing.T) {
	policy := resolve(t, `{
		"servers": {"docs": {"transport": "streamable-http", "url": "https://docs.example.com/mcp"}},
		"capabilities": {"coding": {"requires": ["mcp-tools"]}},
		"grants": {"dev": {"capability": "coding", "servers": {"docs": []}}},
		"gateway": {"enabled": true}
	}`)
	if policy.Allows("dev", "docs", "search") {
		t.Fatal("an empty tool grant authorized a tool")
	}
	workload, _ := policy.Workload("dev")
	if len(workload.Servers) != 0 {
		t.Fatalf("effective servers = %+v", workload.Servers)
	}
}

func TestResolveForWorkloadsRejectsUnknownWorkload(t *testing.T) {
	config, err := Parse(decodeJSON(t, remoteServerConfig))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if _, err := config.ResolveForWorkloads([]string{"other"}); err == nil {
		t.Fatal("ResolveForWorkloads() accepted an unknown workload")
	} else if !strings.Contains(err.Error(), "unknown workload") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := config.ResolveForWorkloads([]string{"dev"}); err != nil {
		t.Fatalf("ResolveForWorkloads() rejected a known workload: %v", err)
	}
}

func TestPolicyIsImmutableToCallers(t *testing.T) {
	policy := resolve(t, remoteServerConfig)

	servers := policy.Servers()
	servers[0].Name = "tampered"
	servers[0].AllowedTools[0] = "tampered"
	if again := policy.Servers(); again[0].Name != "docs" || again[0].AllowedTools[0] != "fetch" {
		t.Fatalf("server state was mutated through the returned slice: %+v", again[0])
	}

	workloads := policy.Workloads()
	workloads[0].Servers[0].Tools[0] = "tampered"
	workloads[0].Requires[0] = "tampered"
	if again := policy.Workloads(); again[0].Servers[0].Tools[0] != "search" || again[0].Requires[0] != CapabilityMCPTools {
		t.Fatalf("workload state was mutated through the returned slice: %+v", again[0])
	}

	single, ok := policy.Workload("dev")
	if !ok {
		t.Fatal("the granted workload is missing from the policy")
	}
	single.Servers[0].Tools[0] = "tampered"
	if again, _ := policy.Workload("dev"); again.Servers[0].Tools[0] != "search" {
		t.Fatalf("workload state was mutated through a single workload lookup: %+v", again)
	}

	server, ok := policy.Server("docs")
	if !ok {
		t.Fatal("the declared server is missing from the policy")
	}
	server.AllowedTools[0] = "tampered"
	if again, _ := policy.Server("docs"); again.AllowedTools[0] != "fetch" {
		t.Fatalf("server state was mutated through a single server lookup: %+v", again)
	}
}

func TestResolveIsDeterministic(t *testing.T) {
	document := `{
		"servers": {
			"zeta": {"transport": "streamable-http", "url": "https://z.example.com/mcp"},
			"alpha": {"transport": "streamable-http", "url": "https://a.example.com/mcp"}
		},
		"capabilities": {"coding": {"requires": ["mcp-tools", "provider-hosted-tools"]}},
		"grants": {
			"zeta-workload": {"capability": "coding", "servers": {"zeta": ["b", "a"]}},
			"alpha-workload": {"capability": "coding", "servers": {"alpha": ["a"]}}
		},
		"gateway": {"enabled": true}
	}`
	reference := resolve(t, document)
	for iteration := 0; iteration < 25; iteration++ {
		again := resolve(t, document)
		if len(again.Servers()) != len(reference.Servers()) {
			t.Fatalf("server count changed between resolutions")
		}
		for index := range again.Servers() {
			if again.Servers()[index].Name != reference.Servers()[index].Name {
				t.Fatalf("server ordering changed between resolutions")
			}
		}
		for index := range again.Workloads() {
			if again.Workloads()[index].Workload != reference.Workloads()[index].Workload {
				t.Fatalf("workload ordering changed between resolutions")
			}
			for serverIndex := range again.Workloads()[index].Servers {
				left := again.Workloads()[index].Servers[serverIndex].Tools
				right := reference.Workloads()[index].Servers[serverIndex].Tools
				if len(left) != len(right) {
					t.Fatalf("tool count changed between resolutions")
				}
				for toolIndex := range left {
					if left[toolIndex] != right[toolIndex] {
						t.Fatalf("tool ordering changed between resolutions")
					}
				}
			}
		}
	}
}

// A resolved policy is shared between callers. Concurrent readers must not
// observe partially initialized state, and no accessor may expose the
// policy's own storage.
func TestPolicyIsSafeForConcurrentReaders(t *testing.T) {
	policy := resolve(t, remoteServerConfig)
	var wait sync.WaitGroup
	for reader := 0; reader < 8; reader++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 200; iteration++ {
				servers := policy.Servers()
				if len(servers) != 1 || servers[0].Name != "docs" {
					t.Errorf("concurrent read observed %+v", servers)
					return
				}
				workload, ok := policy.Workload("dev")
				if !ok || len(workload.Servers) != 1 || workload.Servers[0].Tools[0] != "search" {
					t.Errorf("concurrent read observed %+v", workload)
					return
				}
				if !policy.Allows("dev", "docs", "search") {
					t.Error("concurrent read denied a granted tool")
					return
				}
			}
		}()
	}
	wait.Wait()
}

// Rejected configuration must never echo the rejected value. Operators need
// the failing path, not the secret that was misplaced.
func TestRejectedValuesAreNeverDisclosed(t *testing.T) {
	const sentinel = "sentinel-credential-value"
	documents := map[string]string{
		"credentialRef": `{"servers": {"docs": {"transport": "streamable-http", "url": "https://example.com/mcp", "credentialRef": "sk-` + sentinel + `"}}}`,
		"urlUserinfo":   `{"servers": {"docs": {"transport": "streamable-http", "url": "https://user:` + sentinel + `@example.com/mcp"}}}`,
		"urlQuery":      `{"servers": {"docs": {"transport": "streamable-http", "url": "https://example.com/mcp?token=` + sentinel + `"}}}`,
		"stdioArgument": `{"servers": {"local": {"transport": "stdio", "command": "/bin/x", "args": ["--token=` + sentinel + `"]}}}`,
		"unknownField":  `{"servers": {"docs": {"transport": "stdio", "command": "/bin/x", "headers": {"Authorization": "` + sentinel + `"}}}}`,
	}
	for name, document := range documents {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(decodeJSON(t, document))
			if err == nil {
				t.Fatal("Parse() accepted credential-bearing configuration")
			}
			if strings.Contains(err.Error(), sentinel) {
				t.Fatalf("rejection disclosed the rejected value: %v", err)
			}
		})
	}
}
