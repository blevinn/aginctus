package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfigFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func loadConfig(t *testing.T, loader *Loader, options Options) *Config {
	t.Helper()
	effective, err := loader.Load(options)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return effective
}

const mcpRemoteServer = `{
	"mcp": {
		"servers": {
			"docs": {
				"transport": "streamable-http",
				"url": "https://docs.example.com/mcp",
				"allowedTools": ["fetch", "search"]
			}
		},
		"capabilities": {"coding": {"requires": ["mcp-tools"]}},
		"grants": {"dev": {"capability": "coding", "servers": {"docs": ["search"]}}},
		"gateway": {"enabled": true}
	}
}`

func TestMCPConfigurationDefaultsToNoAccess(t *testing.T) {
	effective := loadConfig(t, &Loader{}, Options{})
	policy, err := effective.MCP()
	if err != nil {
		t.Fatalf("MCP() error = %v", err)
	}
	if len(policy.Workloads()) != 0 {
		t.Fatalf("workloads = %+v, want none", policy.Workloads())
	}
	if len(policy.Servers()) != 0 {
		t.Fatalf("servers = %+v, want none", policy.Servers())
	}
	if policy.Gateway().Enabled {
		t.Fatal("the central gateway MCP intent is enabled by default")
	}
	if policy.Allows("dev", "docs", "search") {
		t.Fatal("the default configuration authorized a tool")
	}
}

func TestMCPConfigurationResolvesGrants(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeConfigFile(t, path, mcpRemoteServer)
	effective := loadConfig(t, &Loader{}, Options{ConfigurationFile: path})

	policy, err := effective.MCP()
	if err != nil {
		t.Fatalf("MCP() error = %v", err)
	}
	if !policy.Allows("dev", "docs", "search") {
		t.Fatal("the granted tool was not authorized")
	}
	if policy.Allows("dev", "docs", "fetch") {
		t.Fatal("a tool outside the workload grant was authorized")
	}
	if policy.Allows("other", "docs", "search") {
		t.Fatal("an ungranted workload was authorized")
	}
}

func TestMCPConfigurationPrecedence(t *testing.T) {
	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.json")
	userPath := filepath.Join(dir, "user.json")
	explicitPath := filepath.Join(dir, "explicit.json")

	mcpPolicy := func(endpoint string) string {
		return fmt.Sprintf(
			`{"servers":{"docs":{"transport":"streamable-http","url":%q}},"capabilities":{"coding":{"requires":["mcp-tools"]}},"grants":{"dev":{"capability":"coding","servers":{"docs":["search"]}}},"gateway":{"enabled":true}}`,
			endpoint,
		)
	}
	writeConfigFile(t, systemPath, `{"mcp":`+mcpPolicy("https://system.example.com/mcp")+`}`)
	writeConfigFile(t, userPath, `{"mcp":`+mcpPolicy("https://user.example.com/mcp")+`}`)
	writeConfigFile(t, explicitPath, `{"mcp":`+mcpPolicy("https://explicit.example.com/mcp")+`}`)

	loader := &Loader{
		SystemPath:  systemPath,
		UserPath:    userPath,
		Environment: []string{"AGINCTUS_MCP=" + mcpPolicy("https://environment.example.com/mcp")},
	}

	endpoint := func(t *testing.T, effective *Config) string {
		t.Helper()
		policy, err := effective.MCP()
		if err != nil {
			t.Fatalf("MCP() error = %v", err)
		}
		server, ok := policy.Server("docs")
		if !ok {
			t.Fatal("the declared server is missing from the policy")
		}
		return server.URL
	}

	effective := loadConfig(t, loader, Options{})
	if got := endpoint(t, effective); got != "https://environment.example.com/mcp" {
		t.Fatalf("environment endpoint = %q", got)
	}
	effective = loadConfig(t, loader, Options{ConfigurationFile: explicitPath})
	if got := endpoint(t, effective); got != "https://explicit.example.com/mcp" {
		t.Fatalf("explicit file endpoint = %q", got)
	}
	effective = loadConfig(t, loader, Options{
		ConfigurationFile: explicitPath,
		Overrides:         []string{"mcp=" + mcpPolicy("https://override.example.com/mcp")},
	})
	if got := endpoint(t, effective); got != "https://override.example.com/mcp" {
		t.Fatalf("override endpoint = %q", got)
	}

	loader.Environment = nil
	effective = loadConfig(t, loader, Options{})
	if got := endpoint(t, effective); got != "https://user.example.com/mcp" {
		t.Fatalf("user endpoint = %q", got)
	}
	loader.UserPath = filepath.Join(dir, "missing.json")
	effective = loadConfig(t, loader, Options{})
	if got := endpoint(t, effective); got != "https://system.example.com/mcp" {
		t.Fatalf("system endpoint = %q", got)
	}
}

func TestMCPInvalidConfigurationFailsLoad(t *testing.T) {
	cases := map[string]string{
		"unknownServer":        `{"mcp":{"servers":{"docs":{"transport":"streamable-http","url":"https://example.com/mcp"}},"grants":{"dev":{"capability":"coding","servers":{"missing":["search"]}}},"capabilities":{"coding":{"requires":["mcp-tools"]}},"gateway":{"enabled":true}}}`,
		"unknownCapability":    `{"mcp":{"servers":{"docs":{"transport":"streamable-http","url":"https://example.com/mcp"}},"grants":{"dev":{"capability":"missing","servers":{"docs":["search"]}}},"gateway":{"enabled":true}}}`,
		"unknownWorkload":      `{"mcp":{"servers":{"docs":{"transport":"streamable-http","url":"https://example.com/mcp"}},"grants":{"other":{"capability":"coding","servers":{"docs":["search"]}}},"capabilities":{"coding":{"requires":["mcp-tools"]}},"gateway":{"enabled":true}}}`,
		"toolOutsideAllowlist": `{"mcp":{"servers":{"docs":{"transport":"streamable-http","url":"https://example.com/mcp","allowedTools":["search"]}},"grants":{"dev":{"capability":"coding","servers":{"docs":["fetch"]}}},"capabilities":{"coding":{"requires":["mcp-tools"]}},"gateway":{"enabled":true}}}`,
		"unsupportedProtocol":  `{"mcp":{"servers":{"docs":{"transport":"stdio","command":"/bin/x"}},"grants":{"dev":{"capability":"coding","servers":{"docs":["search"]}}},"capabilities":{"coding":{"requires":["mcp-tools"]}}}`,
		"unsupportedServer":    `{"mcp":{"servers":{"docs":{"transport":"sse"}},"gateway":{"enabled":true}}}`,
		"unauthenticated":      `{"mcp":{"servers":{"docs":{"transport":"streamable-http","url":"https://example.com/mcp"}},"gateway":{"enabled":true,"requireAuthenticatedWorkload":false}}}`,
		"remoteWithoutGateway": `{"mcp":{"servers":{"docs":{"transport":"streamable-http","url":"https://example.com/mcp"}}}}`,
	}
	for name, document := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			writeConfigFile(t, path, document)
			if _, err := (&Loader{}).Load(Options{ConfigurationFile: path}); err == nil {
				t.Fatal("Load() accepted an invalid MCP configuration")
			}
		})
	}
}

func TestMCPCredentialFieldsRejectedInEverySource(t *testing.T) {
	const secret = "sentinel-credential-value"
	documents := map[string]string{
		"nestedServer":  `{"mcp":{"servers":{"docs":{"transport":"stdio","command":"/bin/x","headers":{"Authorization":"Bearer ` + secret + `"}}}}}`,
		"nestedEnv":     `{"mcp":{"servers":{"docs":{"transport":"stdio","command":"/bin/x","env":{"TOKEN":"` + secret + `"}}}}}`,
		"nestedGateway": `{"mcp":{"gateway":{"token":"` + secret + `"}}}`,
		"nestedGrant":   `{"mcp":{"grants":{"dev":{"credential":"` + secret + `"}}}}`,
		"dottedKey":     `{"mcp.servers.docs.headers":{"Authorization":"Bearer ` + secret + `"}}`,
		"dottedGateway": `{"mcp.gateway.password":"` + secret + `"}`,
		"nullValue":     `{"mcp":{"servers":{"docs":{"transport":"stdio","command":"/bin/x","headers":null}}}}`,
	}
	for name, document := range documents {
		t.Run(name, func(t *testing.T) {
			for _, source := range []string{"system", "user", "explicit"} {
				path := filepath.Join(t.TempDir(), "config.json")
				writeConfigFile(t, path, document)
				loader := &Loader{}
				options := Options{}
				switch source {
				case "system":
					loader.SystemPath = path
				case "user":
					loader.UserPath = path
				case "explicit":
					options.ConfigurationFile = path
				}
				_, err := loader.Load(options)
				if err == nil {
					t.Fatalf("Load() accepted an MCP credential field from the %s source", source)
				}
				if strings.Contains(err.Error(), secret) {
					t.Fatal("the rejection disclosed the rejected value")
				}
			}
		})
	}
}

func TestMCPCredentialFieldsRejectedInEnvironmentAndOverrides(t *testing.T) {
	const secret = "sentinel-credential-value"
	environment := []string{
		"AGINCTUS_MCP_SERVERS_DOCS_HEADERS=" + secret,
		"AGINCTUS_MCP_SERVERS_DOCS_TOKEN=" + secret,
		"AGINCTUS_MCP_GATEWAY_SECRET=" + secret,
		`AGINCTUS_MCP_SERVERS_DOCS={"headers":{"Authorization":"Bearer ` + secret + `"}}`,
		`AGINCTUS_MCP_GRANTS={"dev":{"credential":"` + secret + `"}}`,
	}
	for _, entry := range environment {
		t.Run(entry, func(t *testing.T) {
			_, err := (&Loader{Environment: []string{entry}}).Load(Options{})
			if err == nil {
				t.Fatal("Load() accepted an MCP credential field from the environment")
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatal("the rejection disclosed the rejected value")
			}
		})
	}

	overrides := []string{
		"mcp.servers.docs.headers=" + secret,
		"mcp.servers.docs.token=" + secret,
		"mcp.gateway.secret=" + secret,
		`mcp.servers.docs={"headers":{"Authorization":"Bearer ` + secret + `"}}`,
		`mcp.grants={"dev":{"credential":"` + secret + `"}}`,
	}
	for _, override := range overrides {
		t.Run(override, func(t *testing.T) {
			_, err := (&Loader{}).Load(Options{Overrides: []string{override}})
			if err == nil {
				t.Fatal("Load() accepted an MCP credential field override")
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatal("the rejection disclosed the rejected value")
			}
		})
	}
}

// A lower-precedence source cannot smuggle a credential past a later
// override: the loader deep-merges objects, so the forbidden field would
// otherwise survive in the effective configuration.
func TestMCPCredentialFieldRejectedBeforeALaterOverride(t *testing.T) {
	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.json")
	userPath := filepath.Join(dir, "user.json")
	writeConfigFile(t, systemPath, `{"mcp":{"servers":{"docs":{"transport":"stdio","command":"/bin/x","headers":{"Authorization":"Bearer x"}}}}}`)
	writeConfigFile(t, userPath, `{"mcp":{"servers":{"docs":{"transport":"stdio","command":"/bin/y"}}}}`)

	_, err := (&Loader{SystemPath: systemPath, UserPath: userPath}).Load(Options{})
	if err == nil {
		t.Fatal("Load() accepted a credential field that a later override did not remove")
	}
}

// MCP is an atomic configuration subtree. A source supplies the complete
// policy and replaces lower-priority MCP policy, while ordinary settings
// retain the loader's existing recursive object merge.
func TestMCPAtomicSubtreeSemantics(t *testing.T) {
	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.json")
	userPath := filepath.Join(dir, "user.json")
	writeConfigFile(t, systemPath, `{"mcp":{
		"servers":{
			"docs":{"transport":"streamable-http","url":"https://system.example.com/mcp","allowedTools":["fetch","search"]},
			"system-only":{"transport":"stdio","command":"/usr/bin/system-only"}
		},
		"capabilities":{"coding":{"requires":["mcp-tools"]}},
		"grants":{"dev":{"capability":"coding","servers":{"docs":["search"]}}},
		"gateway":{"enabled":true}
	}}`)
	writeConfigFile(t, userPath, `{"mcp":{
		"servers":{"docs":{"transport":"streamable-http","url":"https://user.example.com/mcp","allowedTools":["fetch"]}},
		"capabilities":{"personal":{"requires":["mcp-tools"]}},
		"grants":{"dev":{"capability":"personal","servers":{"docs":["fetch"]}}},
		"gateway":{"enabled":true}
	}}`)

	loader := &Loader{SystemPath: systemPath, UserPath: userPath}

	// The higher-precedence MCP subtree replaces the lower one as a unit:
	// the endpoint and grants agree, and the lower-only server disappears.
	effective := loadConfig(t, loader, Options{})
	policy, err := effective.MCP()
	if err != nil {
		t.Fatalf("MCP() error = %v", err)
	}
	if !policy.Allows("dev", "docs", "fetch") {
		t.Fatal("the replacement policy's grant was not authorized")
	}
	if policy.Allows("dev", "docs", "search") {
		t.Fatal("a tool from the lower-priority policy remained authorized")
	}
	if server, ok := policy.Server("docs"); !ok || server.URL != "https://user.example.com/mcp" {
		t.Fatalf("effective endpoint = %+v, %v", server, ok)
	}
	if _, ok := policy.Server("system-only"); ok {
		t.Fatal("a server from the lower-priority policy survived atomic replacement")
	}

	// Both null and an empty object replace the inherited subtree. Null
	// selects the schema's deny-by-default state; an empty object does too.
	effective = loadConfig(t, loader, Options{
		Overrides: []string{"mcp=null"},
	})
	policy, err = effective.MCP()
	if err != nil {
		t.Fatalf("MCP() error = %v", err)
	}
	if len(policy.Servers()) != 0 || len(policy.Workloads()) != 0 {
		t.Fatalf("a null MCP policy did not clear the inherited subtree: servers=%+v workloads=%+v", policy.Servers(), policy.Workloads())
	}
	effective = loadConfig(t, loader, Options{
		Overrides: []string{"mcp={}"},
	})
	policy, err = effective.MCP()
	if err != nil {
		t.Fatalf("MCP() error = %v", err)
	}
	if len(policy.Servers()) != 0 || len(policy.Workloads()) != 0 {
		t.Fatalf("an empty MCP object did not clear the inherited subtree: servers=%+v workloads=%+v", policy.Servers(), policy.Workloads())
	}
}

func TestMCPAtomicSubtreeRequiresACompleteHigherPriorityPolicy(t *testing.T) {
	systemPath := filepath.Join(t.TempDir(), "system.json")
	writeConfigFile(t, systemPath, mcpRemoteServer)
	loader := &Loader{
		SystemPath: systemPath,
		Environment: []string{
			"AGINCTUS_MCP_SERVERS_DOCS_URL=https://replacement.example.com/mcp",
		},
	}
	if _, err := loader.Load(Options{}); err == nil {
		t.Fatal("a partial higher-priority MCP value merged into the lower-priority policy")
	}
}

func TestMCPEnvironmentOverlayIsDeterministic(t *testing.T) {
	policyJSON := `{"servers":{"docs":{"transport":"streamable-http","url":"https://base.example.com/mcp"}},"capabilities":{"coding":{"requires":["mcp-tools"]}},"grants":{"dev":{"capability":"coding","servers":{"docs":["search"]}}},"gateway":{"enabled":true}}`
	entries := []string{
		"AGINCTUS_MCP=" + policyJSON,
		"AGINCTUS_MCP_SERVERS_DOCS_URL=https://override.example.com/mcp",
	}
	for _, environment := range [][]string{entries, {entries[1], entries[0]}} {
		effective := loadConfig(t, &Loader{Environment: environment}, Options{})
		policy, err := effective.MCP()
		if err != nil {
			t.Fatalf("MCP() error = %v", err)
		}
		server, ok := policy.Server("docs")
		if !ok || server.URL != "https://override.example.com/mcp" {
			t.Fatalf("environment order produced server %+v, %v", server, ok)
		}
	}
}

func TestMCPAtomicSubtreeGroupsCLIOverridesAndLeavesOtherSettingsRecursive(t *testing.T) {
	systemPath := filepath.Join(t.TempDir(), "system.json")
	writeConfigFile(t, systemPath, `{"firewall":{"zone":"system-zone"}}`)
	loader := &Loader{SystemPath: systemPath}
	effective := loadConfig(t, loader, Options{Overrides: []string{
		"firewall.egress.uplinkZone=public-zone",
		"mcp.servers.docs.transport=streamable-http",
		"mcp.servers.docs.url=https://docs.example.com/mcp",
		"mcp.capabilities.coding.requires=[\"mcp-tools\"]",
		"mcp.grants.dev.capability=coding",
		"mcp.grants.dev.servers.docs=[\"search\"]",
		"mcp.gateway.enabled=true",
	}})
	if got, _ := effective.Get("firewall.zone"); got != "system-zone" {
		t.Fatalf("ordinary object merge lost the lower-priority firewall zone: %#v", got)
	}
	if got, _ := effective.Get("firewall.egress.uplinkZone"); got != "public-zone" {
		t.Fatalf("ordinary override was not applied: %#v", got)
	}
	policy, err := effective.MCP()
	if err != nil {
		t.Fatalf("MCP() error = %v", err)
	}
	if !policy.Allows("dev", "docs", "search") {
		t.Fatal("grouped command-line overrides did not form a complete MCP policy")
	}
}

func TestMCPWorkloadRegistryDrivesGrantValidation(t *testing.T) {
	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.json")
	writeConfigFile(t, systemPath, `{
		"mcp": {
			"servers": {"docs": {"transport": "streamable-http", "url": "https://docs.example.com/mcp"}},
			"capabilities": {"coding": {"requires": ["mcp-tools"]}},
			"grants": {"dev": {"capability": "coding", "servers": {"docs": ["search"]}}},
			"gateway": {"enabled": true}
		},
		"workloads": {"dev": {"runtime": {"type": "opencode"}}}
	}`)
	effective := loadConfig(t, &Loader{SystemPath: systemPath}, Options{})
	policy, err := effective.MCP()
	if err != nil {
		t.Fatalf("MCP() error = %v", err)
	}
	if !policy.Allows("dev", "docs", "search") {
		t.Fatal("the grant of a declared workload was not authorized")
	}

	// A workload that the configuration does not declare is rejected.
	_, err = (&Loader{SystemPath: systemPath}).Load(Options{
		Overrides: []string{`mcp.grants.other={"capability":"coding","servers":{"docs":["search"]}}`},
	})
	if err == nil {
		t.Fatal("Load() accepted a grant for an undeclared workload")
	}
}

func TestMCPEffectiveConfigurationIsSerializableWithoutCredentials(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeConfigFile(t, path, mcpRemoteServer)
	effective := loadConfig(t, &Loader{}, Options{ConfigurationFile: path})
	data, err := json.Marshal(effective.Values())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "docs.example.com") {
		t.Fatalf("the effective configuration lost the declared endpoint: %s", data)
	}
}

// The v0.1 configuration contract is unchanged: a configuration without any
// MCP declaration loads and resolves exactly as before.
func TestMCPAbsentConfigurationPreservesV01Behavior(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeConfigFile(t, path, `{"incus":{"project":"aginctus"},"gateway":{"id":"local"}}`)
	effective := loadConfig(t, &Loader{}, Options{ConfigurationFile: path})

	scope, err := effective.IncusProjectScope()
	if err != nil {
		t.Fatalf("IncusProjectScope() error = %v", err)
	}
	if scope.Name != "aginctus" {
		t.Fatalf("project scope = %q", scope.Name)
	}
	if got, _ := effective.String("gateway.id"); got != "local" {
		t.Fatalf("gateway.id = %q", got)
	}
	policy, err := effective.MCP()
	if err != nil {
		t.Fatalf("MCP() error = %v", err)
	}
	if len(policy.Workloads()) != 0 || len(policy.Servers()) != 0 {
		t.Fatalf("an absent MCP declaration produced policy: %+v", policy)
	}
}

// A configuration without an MCP declaration keeps its previous failure
// surface. A malformed workload section is not an MCP concern and must not
// become a new reason for the loader to refuse the configuration.
func TestMCPAbsentDeclarationDoesNotChangeV01FailureSurface(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeConfigFile(t, path, `{"workloads":"not-an-object"}`)
	effective := loadConfig(t, &Loader{}, Options{ConfigurationFile: path})
	policy, err := effective.MCP()
	if err != nil {
		t.Fatalf("MCP() error = %v", err)
	}
	if len(policy.Workloads()) != 0 {
		t.Fatalf("workloads = %+v", policy.Workloads())
	}
}
