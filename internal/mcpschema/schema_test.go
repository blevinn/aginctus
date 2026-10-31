package mcpschema

import (
	"encoding/json"
	"strings"
	"testing"
)

// decodeJSON returns the parsed document. Test documents are the "mcp"
// configuration subtree itself.
func decodeJSON(t *testing.T, document string) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(document), &decoded); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return decoded
}

func TestParseAbsentConfigurationIsDenyByDefault(t *testing.T) {
	config, err := Parse(nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(config.Servers) != 0 || len(config.Grants) != 0 || len(config.Capabilities) != 0 {
		t.Fatalf("absent configuration declared servers, grants or capabilities: %+v", config)
	}
	if config.Gateway.Enabled {
		t.Fatal("absent configuration enabled the central gateway MCP intent")
	}
	if !config.Gateway.RequireAuthenticatedWorkload {
		t.Fatal("absent configuration disabled the authenticated workload requirement")
	}
	if config.Gateway.UpstreamTimeoutSeconds != DefaultUpstreamTimeoutSeconds {
		t.Fatalf("upstream timeout = %d, want %d", config.Gateway.UpstreamTimeoutSeconds, DefaultUpstreamTimeoutSeconds)
	}
}

func TestParseFullConfiguration(t *testing.T) {
	config, err := Parse(decodeJSON(t, `{
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
			"dev": {"capability": "coding", "servers": {"docs": ["search"]}}
		},
		"gateway": {
			"enabled": true,
			"requireAuthenticatedWorkload": true,
			"upstreamTimeoutSeconds": 45
		}
	}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(config.Servers) != 2 {
		t.Fatalf("servers = %d, want 2", len(config.Servers))
	}
	if config.Servers[0].Name != "docs" || config.Servers[1].Name != "local" {
		t.Fatalf("servers are not ordered by name: %+v", config.Servers)
	}
	docs := config.Servers[0]
	if docs.Transport != TransportStreamableHTTP || docs.URL != "https://docs.example.com/mcp" {
		t.Fatalf("remote server = %+v", docs)
	}
	if docs.CredentialRef != "docs-token" {
		t.Fatalf("credential reference = %q", docs.CredentialRef)
	}
	if !docs.RestrictsTools() {
		t.Fatal("declared tool allowlist was not preserved")
	}
	local := config.Servers[1]
	if local.Transport != TransportStdio || local.Command != "/usr/bin/mcp-local" {
		t.Fatalf("local server = %+v", local)
	}
	if local.RestrictsTools() {
		t.Fatal("absent tool allowlist must not restrict the server")
	}
	if len(config.Grants) != 1 || config.Grants[0].Workload != "dev" {
		t.Fatalf("grants = %+v", config.Grants)
	}
	if config.Grants[0].Capability != "coding" {
		t.Fatalf("grant capability = %q", config.Grants[0].Capability)
	}
	if len(config.Grants[0].Servers) != 1 || config.Grants[0].Servers[0].Server != "docs" {
		t.Fatalf("grant servers = %+v", config.Grants[0].Servers)
	}
	if len(config.Grants[0].Servers[0].Tools) != 1 || config.Grants[0].Servers[0].Tools[0] != "search" {
		t.Fatalf("grant tools = %+v", config.Grants[0].Servers[0].Tools)
	}
	if config.Gateway.UpstreamTimeoutSeconds != 45 || !config.Gateway.Enabled {
		t.Fatalf("gateway intent = %+v", config.Gateway)
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	cases := map[string]string{
		"root":          `{"mcpServers": {}}`,
		"server":        `{"servers": {"docs": {"transport": "stdio", "command": "/bin/x", "headers": {"Authorization": "Bearer x"}}}}`,
		"serverEnv":     `{"servers": {"docs": {"transport": "stdio", "command": "/bin/x", "env": {"TOKEN": "x"}}}}`,
		"grant":         `{"grants": {"dev": {"unknown": true}}}`,
		"capability":    `{"capabilities": {"coding": {"resources": true}}}`,
		"gateway":       `{"gateway": {"secretRef": "x"}}`,
		"caseMismatch":  `{"Servers": {}}`,
		"nestedUnknown": `{"servers": {"docs": {"transport": "stdio", "command": "/bin/x", "extra": true}}}`,
	}
	for name, document := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(decodeJSON(t, document))
			if err == nil {
				t.Fatal("Parse() accepted an unknown field")
			}
			if !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("error does not name the unknown field: %v", err)
			}
		})
	}
}

func TestParseRejectsUnsupportedTransportsAndCombinations(t *testing.T) {
	cases := map[string]string{
		"unknownTransport":   `{"servers": {"docs": {"transport": "sse"}}}`,
		"missingTransport":   `{"servers": {"docs": {"url": "https://example.com/mcp"}}}`,
		"missingURL":         `{"servers": {"docs": {"transport": "streamable-http"}}}`,
		"missingCommand":     `{"servers": {"local": {"transport": "stdio"}}}`,
		"urlOnStdio":         `{"servers": {"local": {"transport": "stdio", "command": "/bin/x", "url": "https://example.com/mcp"}}}`,
		"commandOnRemote":    `{"servers": {"docs": {"transport": "streamable-http", "url": "https://example.com/mcp", "command": "/bin/x"}}}`,
		"argsOnRemote":       `{"servers": {"docs": {"transport": "streamable-http", "url": "https://example.com/mcp", "args": ["--x"]}}}`,
		"relativeCommand":    `{"servers": {"local": {"transport": "stdio", "command": "mcp-local"}}}`,
		"shellCommand":       `{"servers": {"local": {"transport": "stdio", "command": "/bin/x; rm -rf /"}}}`,
		"duplicateAllowlist": `{"servers": {"docs": {"transport": "streamable-http", "url": "https://example.com/mcp", "allowedTools": ["a", "a"]}}}`,
		"invalidToolName":    `{"servers": {"docs": {"transport": "streamable-http", "url": "https://example.com/mcp", "allowedTools": ["bad tool"]}}}`,
	}
	for name, document := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(decodeJSON(t, document)); err == nil {
				t.Fatal("Parse() accepted an invalid server declaration")
			}
		})
	}
}

func TestParseRejectsInsecureRemoteEndpoints(t *testing.T) {
	cases := map[string]string{
		"plainHTTP":       `{"servers": {"docs": {"transport": "streamable-http", "url": "http://example.com/mcp"}}}`,
		"userinfo":        `{"servers": {"docs": {"transport": "streamable-http", "url": "https://user:secret@example.com/mcp"}}}`,
		"fragment":        `{"servers": {"docs": {"transport": "streamable-http", "url": "https://example.com/mcp#frag"}}}`,
		"missingHost":     `{"servers": {"docs": {"transport": "streamable-http", "url": "https:///mcp"}}}`,
		"credentialQuery": `{"servers": {"docs": {"transport": "streamable-http", "url": "https://example.com/mcp?token=sk-live"}}}`,
		"opaque":          `{"servers": {"docs": {"transport": "streamable-http", "url": "https:example.com"}}}`,
	}
	for name, document := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(decodeJSON(t, document))
			if err == nil {
				t.Fatal("Parse() accepted an insecure remote endpoint")
			}
		})
	}
}

func TestParseAllowsLoopbackPlaintextEndpoint(t *testing.T) {
	for _, endpoint := range []string{"http://127.0.0.1:9000/mcp", "http://localhost:9000/mcp", "http://[::1]:9000/mcp"} {
		document := `{"servers": {"local-gateway": {"transport": "streamable-http", "url": "` + endpoint + `"}}}`
		if _, err := Parse(decodeJSON(t, document)); err != nil {
			t.Fatalf("Parse(%s) error = %v", endpoint, err)
		}
	}
}

func TestParseRejectsCredentialBearingReferences(t *testing.T) {
	cases := map[string]string{
		"uppercase":      `{"servers": {"docs": {"transport": "streamable-http", "url": "https://example.com/mcp", "credentialRef": "Docs-Token"}}}`,
		"pathLike":       `{"servers": {"docs": {"transport": "streamable-http", "url": "https://example.com/mcp", "credentialRef": "/run/secrets/token"}}}`,
		"urlLike":        `{"servers": {"docs": {"transport": "streamable-http", "url": "https://example.com/mcp", "credentialRef": "https://example.com/token"}}}`,
		"plaintextKey":   `{"servers": {"docs": {"transport": "streamable-http", "url": "https://example.com/mcp", "credentialRef": "sk-live"}}}`,
		"plaintextJWT":   `{"servers": {"docs": {"transport": "streamable-http", "url": "https://example.com/mcp", "credentialRef": "eyJhbGciOi"}}}`,
		"inlineArgument": `{"servers": {"local": {"transport": "stdio", "command": "/bin/x", "args": ["--token=sentinel-value"]}}}`,
		"bareArgument":   `{"servers": {"local": {"transport": "stdio", "command": "/bin/x", "args": ["sk-sentinel-value"]}}}`,
	}
	for name, document := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(decodeJSON(t, document))
			if err == nil {
				t.Fatal("Parse() accepted credential-bearing configuration")
			}
			if strings.Contains(err.Error(), "sentinel-value") || strings.Contains(err.Error(), "sk-live") {
				t.Fatalf("rejection disclosed credential material: %v", err)
			}
		})
	}
}

func TestParseAllowsFilePathArgument(t *testing.T) {
	document := `{"servers": {"local": {"transport": "stdio", "command": "/bin/x", "args": ["--api-key-file=/run/secrets/key"]}}}`
	if _, err := Parse(decodeJSON(t, document)); err != nil {
		t.Fatalf("Parse() rejected a file path argument: %v", err)
	}
}

func TestParseRejectsWrongTypes(t *testing.T) {
	cases := map[string]string{
		"serversNotObject":  `{"servers": []}`,
		"serverNotObject":   `{"servers": {"docs": "stdio"}}`,
		"transportNotText":  `{"servers": {"docs": {"transport": 3}}}`,
		"toolsNotList":      `{"servers": {"docs": {"transport": "streamable-http", "url": "https://example.com/mcp", "allowedTools": "search"}}}`,
		"toolNotText":       `{"servers": {"docs": {"transport": "streamable-http", "url": "https://example.com/mcp", "allowedTools": [1]}}}`,
		"enabledNotBoolean": `{"gateway": {"enabled": "yes"}}`,
		"timeoutNotNumber":  `{"gateway": {"upstreamTimeoutSeconds": "30"}}`,
		"timeoutFractional": `{"gateway": {"upstreamTimeoutSeconds": 1.5}}`,
		"grantsNotObject":   `{"grants": []}`,
		"grantToolsNotList": `{"grants": {"dev": {"servers": {"docs": {"tools": ["search"]}}}}}`,
		"nullToolList":      `{"servers": {"docs": {"transport": "streamable-http", "url": "https://example.com/mcp", "allowedTools": null}}}`,
	}
	for name, document := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(decodeJSON(t, document)); err == nil {
				t.Fatal("Parse() accepted an invalid value type")
			}
		})
	}
}

func TestParseRejectsUnsafeNames(t *testing.T) {
	cases := map[string]string{
		"uppercaseServer":  `{"servers": {"Docs": {"transport": "stdio", "command": "/bin/x"}}}`,
		"spacedServer":     `{"servers": {"my docs": {"transport": "stdio", "command": "/bin/x"}}}`,
		"dottedServer":     `{"servers": {"my.docs": {"transport": "stdio", "command": "/bin/x"}}}`,
		"slashServer":      `{"servers": {"my/docs": {"transport": "stdio", "command": "/bin/x"}}}`,
		"uppercaseGrant":   `{"grants": {"Dev": {"capability": "coding"}}}`,
		"spacedGrant":      `{"grants": {"my dev": {"capability": "coding"}}}`,
		"uppercaseProfile": `{"capabilities": {"Coding": {"requires": ["mcp-tools"]}}}`,
	}
	for name, document := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(decodeJSON(t, document)); err == nil {
				t.Fatal("Parse() accepted an unsafe name")
			}
		})
	}
}

func TestParseNullRevokesInheritedDeclarations(t *testing.T) {
	config, err := Parse(decodeJSON(t, `{
		"servers": {"docs": null, "local": {"transport": "stdio", "command": "/bin/x"}},
		"capabilities": {"coding": null},
		"grants": {"dev": null}
	}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(config.Servers) != 1 || config.Servers[0].Name != "local" {
		t.Fatalf("servers = %+v", config.Servers)
	}
	if len(config.Capabilities) != 0 {
		t.Fatalf("capabilities = %+v", config.Capabilities)
	}
	if len(config.Grants) != 0 {
		t.Fatalf("grants = %+v", config.Grants)
	}
}

func TestParseIsDeterministic(t *testing.T) {
	document := `{
		"servers": {
			"zeta": {"transport": "streamable-http", "url": "https://z.example.com/mcp"},
			"alpha": {"transport": "streamable-http", "url": "https://a.example.com/mcp"},
			"mid": {"transport": "streamable-http", "url": "https://m.example.com/mcp"}
		},
		"capabilities": {"zeta-cap": {"requires": ["mcp-tools"]}, "alpha-cap": {"requires": ["mcp-tools"]}},
		"grants": {
			"zeta-workload": {"capability": "zeta-cap", "servers": {"zeta": ["b", "a"]}},
			"alpha-workload": {"capability": "alpha-cap", "servers": {"alpha": ["a"]}}
		}
	}`
	first, err := Parse(decodeJSON(t, document))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	for iteration := 0; iteration < 25; iteration++ {
		again, err := Parse(decodeJSON(t, document))
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
		if len(again.Servers) != len(first.Servers) || again.Servers[0].Name != first.Servers[0].Name {
			t.Fatalf("server ordering is not deterministic: %+v vs %+v", again.Servers, first.Servers)
		}
		if len(again.Grants) != len(first.Grants) || again.Grants[0].Workload != first.Grants[0].Workload {
			t.Fatalf("grant ordering is not deterministic: %+v vs %+v", again.Grants, first.Grants)
		}
	}
}
