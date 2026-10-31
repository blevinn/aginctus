// Package mcpschema defines Aginctus' normalized MCP configuration schema,
// validates it and resolves it into a deterministic effective policy.
//
// The package is deliberately pure. It parses untyped configuration values,
// validates them and resolves immutable policy objects. It never opens files,
// resolves credential references, starts processes, performs network I/O or
// reads protected credential contents.
//
// The resolved policy describes declared authorization only. It is not proof
// of enforcement: runtime identity binding and per-invocation authorization
// belong to the gateway work that consumes this package.
package mcpschema

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Transport identifies how a workload reaches a declared MCP server.
type Transport string

const (
	// TransportStreamableHTTP is a remote server reached over Streamable
	// HTTP. The central gateway owns the connection and its credentials.
	TransportStreamableHTTP Transport = "streamable-http"
	// TransportStdio is a local process started inside an authorized
	// workload. The central gateway never executes a stdio server.
	TransportStdio Transport = "stdio"
)

// Protocol identifies the model-facing protocol a capability profile targets.
type Protocol string

const (
	// ProtocolResponses is the OpenAI Responses API.
	ProtocolResponses Protocol = "responses"
	// ProtocolChatCompletions is the OpenAI Chat Completions API.
	ProtocolChatCompletions Protocol = "chat-completions"
)

const (
	// CapabilityMCPTools declares a requirement for centrally enforced MCP
	// tool calls. A workload holding MCP server grants must require it.
	CapabilityMCPTools = "mcp-tools"
	// CapabilityProviderHostedTools declares a requirement for tools that
	// the model provider executes. Those calls never reach the Aginctus
	// gateway and are never MCP server grants.
	CapabilityProviderHostedTools = "provider-hosted-tools"
)

// DefaultUpstreamTimeoutSeconds bounds a single upstream MCP call.
const DefaultUpstreamTimeoutSeconds = 30

// MaxUpstreamTimeoutSeconds bounds the configurable upstream call timeout.
const MaxUpstreamTimeoutSeconds = 300

// Server is one declared MCP server.
type Server struct {
	// Name is the stable identifier used by grants and by gateway routes.
	Name string
	// Transport selects the remote or workload-local delivery mode.
	Transport Transport
	// URL is the Streamable HTTP endpoint. Set for TransportStreamableHTTP.
	URL string
	// Command is the absolute executable path. Set for TransportStdio.
	Command string
	// Args are the stdio process arguments. Set for TransportStdio.
	Args []string
	// CredentialRef names protected state that holds the upstream
	// credential. It is an opaque identifier, never credential material.
	CredentialRef string
	// AllowedTools declares the server's tool surface. A nil list means the
	// server declares no server-level restriction; a non-nil list restricts
	// the server to exactly the named tools.
	AllowedTools []string
}

// RestrictsTools reports whether the server declares a server-level tool
// restriction. The distinction between an absent and an empty allowlist is
// load-bearing: absent means unrestricted at the server level, empty means
// no tool is permitted at all.
func (s Server) RestrictsTools() bool {
	return s.AllowedTools != nil
}

// CapabilityProfile declares the model and protocol capabilities a workload
// requires. Profiles are inert declarations; they grant nothing themselves.
type CapabilityProfile struct {
	// Name is the stable identifier referenced by grants.
	Name string
	// Protocol is the model-facing protocol the profile targets.
	Protocol Protocol
	// Requires lists the required capability tokens.
	Requires []string
}

// ServerGrant is one workload's explicit grant of one server and the
// explicitly named tools it may call.
type ServerGrant struct {
	// Server is the declared server name.
	Server string
	// Tools lists the explicitly granted tool names.
	Tools []string
}

// Grant is one workload's declared MCP grants.
type Grant struct {
	// Workload is the workload identity the grants belong to.
	Workload string
	// Capability names the capability profile the workload requires.
	Capability string
	// Servers lists the explicitly granted servers.
	Servers []ServerGrant
}

// GatewayIntent declares the central gateway MCP intent. This schema states
// the intent; it does not create an endpoint, listener or route.
type GatewayIntent struct {
	// Enabled declares that the central gateway MCP endpoint is intended to
	// be exposed and to route the declared remote servers.
	Enabled bool
	// RequireAuthenticatedWorkload declares that the gateway must bind
	// requests to an authenticated workload identity. It cannot be disabled.
	RequireAuthenticatedWorkload bool
	// UpstreamTimeoutSeconds bounds one upstream MCP call.
	UpstreamTimeoutSeconds int
}

// Config is the parsed MCP configuration. It is the unvalidated result of
// Parse; Resolve validates it and produces the effective policy.
type Config struct {
	// Servers lists the declared servers, ordered by name.
	Servers []Server
	// Capabilities lists the declared capability profiles, ordered by name.
	Capabilities []CapabilityProfile
	// Grants lists the declared workload grants, ordered by workload.
	Grants []Grant
	// Gateway declares the central gateway MCP intent.
	Gateway GatewayIntent
}

var (
	serverNamePattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
	workloadIDPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)
	profileNamePattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)
	toolNamePattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	credentialRefPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

// plaintextCredentialPrefixes lists well-known credential formats. A
// declarative reference starting with one of them is refused so an operator
// cannot paste upstream credential material into configuration.
var plaintextCredentialPrefixes = []string{
	"sk-", "sk_", "pk-", "pk_", "rk-", "rk_",
	"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_",
	"xoxb-", "xoxp-", "xoxa-", "xoxr-",
	"akia", "asia", "aiza", "ya29.", "eyj",
}

// inlineCredentialArgument matches an argument that assigns a credential
// value inline, for example "--token=value".
var inlineCredentialArgument = regexp.MustCompile(`(?i)^--[^=]*(token|secret|password|passwd|api[-_]?key|credential)[^=]*=.+$`)

// Parse converts the untyped "mcp" configuration subtree into typed values.
//
// Unknown fields are refused instead of ignored so that a credential-bearing
// key can never silently become part of the policy. An absent or null
// subtree yields the deny-by-default configuration.
//
// A null value revokes an inherited declaration: "mcp.servers.<name>",
// "mcp.capabilities.<name>", "mcp.grants.<workload>" and
// "mcp.grants.<workload>.servers.<server>" all accept null to withdraw the
// declaration that a lower-precedence source contributed.
func Parse(raw any) (Config, error) {
	config := Config{
		Gateway: GatewayIntent{
			RequireAuthenticatedWorkload: true,
			UpstreamTimeoutSeconds:       DefaultUpstreamTimeoutSeconds,
		},
	}
	if raw == nil {
		return config, nil
	}

	object, err := decodeObject("mcp", raw, []string{"capabilities", "gateway", "grants", "servers"})
	if err != nil {
		return Config{}, err
	}

	if value, ok := object["servers"]; ok && value != nil {
		servers, err := parseServers("mcp.servers", value)
		if err != nil {
			return Config{}, err
		}
		config.Servers = servers
	}
	if value, ok := object["capabilities"]; ok && value != nil {
		profiles, err := parseCapabilities("mcp.capabilities", value)
		if err != nil {
			return Config{}, err
		}
		config.Capabilities = profiles
	}
	if value, ok := object["grants"]; ok && value != nil {
		grants, err := parseGrants("mcp.grants", value)
		if err != nil {
			return Config{}, err
		}
		config.Grants = grants
	}
	if value, ok := object["gateway"]; ok && value != nil {
		gateway, err := parseGateway("mcp.gateway", value)
		if err != nil {
			return Config{}, err
		}
		config.Gateway = gateway
	}
	return config, nil
}

func parseServers(path string, raw any) ([]Server, error) {
	object, err := decodeObject(path, raw, nil)
	if err != nil {
		return nil, err
	}
	names := sortedKeys(object)
	servers := make([]Server, 0, len(names))
	for _, name := range names {
		serverPath := path + "." + name
		if !serverNamePattern.MatchString(name) {
			return nil, fmt.Errorf("%s: server name must match %s", serverPath, serverNamePattern)
		}
		if object[name] == nil {
			// Explicitly revoked: the server is not declared.
			continue
		}
		fields, err := decodeObject(serverPath, object[name], []string{
			"allowedTools", "args", "command", "credentialRef", "transport", "url",
		})
		if err != nil {
			return nil, err
		}
		server := Server{Name: name}
		transport, err := decodeString(serverPath+".transport", fields["transport"])
		if err != nil {
			return nil, err
		}
		server.Transport = Transport(transport)
		switch server.Transport {
		case TransportStreamableHTTP, TransportStdio:
		default:
			return nil, fmt.Errorf(
				"%s.transport: unsupported transport %q, want %q or %q",
				serverPath, transport, TransportStreamableHTTP, TransportStdio,
			)
		}
		if value, ok := fields["url"]; ok {
			remote, err := decodeString(serverPath+".url", value)
			if err != nil {
				return nil, err
			}
			server.URL = remote
		}
		if value, ok := fields["command"]; ok {
			command, err := decodeString(serverPath+".command", value)
			if err != nil {
				return nil, err
			}
			server.Command = command
		}
		if value, ok := fields["args"]; ok {
			args, err := decodeStringList(serverPath+".args", value)
			if err != nil {
				return nil, err
			}
			server.Args = args
		}
		if value, ok := fields["credentialRef"]; ok {
			reference, err := decodeString(serverPath+".credentialRef", value)
			if err != nil {
				return nil, err
			}
			server.CredentialRef = reference
		}
		if value, ok := fields["allowedTools"]; ok {
			tools, err := decodeStringList(serverPath+".allowedTools", value)
			if err != nil {
				return nil, err
			}
			server.AllowedTools = tools
		}
		if err := validateServerShape(server); err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	return servers, nil
}

func parseCapabilities(path string, raw any) ([]CapabilityProfile, error) {
	object, err := decodeObject(path, raw, nil)
	if err != nil {
		return nil, err
	}
	names := sortedKeys(object)
	profiles := make([]CapabilityProfile, 0, len(names))
	for _, name := range names {
		profilePath := path + "." + name
		if !profileNamePattern.MatchString(name) {
			return nil, fmt.Errorf("%s: capability name must match %s", profilePath, profileNamePattern)
		}
		if object[name] == nil {
			// Explicitly revoked: the profile is not declared.
			continue
		}
		fields, err := decodeObject(profilePath, object[name], []string{"protocol", "requires"})
		if err != nil {
			return nil, err
		}
		profile := CapabilityProfile{Name: name, Protocol: ProtocolResponses}
		if value, ok := fields["protocol"]; ok {
			protocol, err := decodeString(profilePath+".protocol", value)
			if err != nil {
				return nil, err
			}
			profile.Protocol = Protocol(protocol)
		}
		switch profile.Protocol {
		case ProtocolResponses, ProtocolChatCompletions:
		default:
			return nil, fmt.Errorf(
				"%s.protocol: unsupported protocol %q, want %q or %q",
				profilePath, profile.Protocol, ProtocolResponses, ProtocolChatCompletions,
			)
		}
		if value, ok := fields["requires"]; ok {
			requires, err := decodeStringList(profilePath+".requires", value)
			if err != nil {
				return nil, err
			}
			profile.Requires = requires
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

func parseGrants(path string, raw any) ([]Grant, error) {
	object, err := decodeObject(path, raw, nil)
	if err != nil {
		return nil, err
	}
	names := sortedKeys(object)
	grants := make([]Grant, 0, len(names))
	for _, workload := range names {
		grantPath := path + "." + workload
		if !workloadIDPattern.MatchString(workload) {
			return nil, fmt.Errorf("%s: workload identity must match %s", grantPath, workloadIDPattern)
		}
		if object[workload] == nil {
			// Explicitly revoked: the workload holds no grants.
			continue
		}
		fields, err := decodeObject(grantPath, object[workload], []string{"capability", "servers"})
		if err != nil {
			return nil, err
		}
		grant := Grant{Workload: workload}
		if value, ok := fields["capability"]; ok {
			capability, err := decodeString(grantPath+".capability", value)
			if err != nil {
				return nil, err
			}
			grant.Capability = capability
		}
		if value, ok := fields["servers"]; ok && value != nil {
			servers, err := parseGrantServers(grantPath+".servers", value)
			if err != nil {
				return nil, err
			}
			grant.Servers = servers
		}
		grants = append(grants, grant)
	}
	return grants, nil
}

func parseGrantServers(path string, raw any) ([]ServerGrant, error) {
	object, err := decodeObject(path, raw, nil)
	if err != nil {
		return nil, err
	}
	names := sortedKeys(object)
	grants := make([]ServerGrant, 0, len(names))
	for _, name := range names {
		grantPath := path + "." + name
		if !serverNamePattern.MatchString(name) {
			return nil, fmt.Errorf("%s: server name must match %s", grantPath, serverNamePattern)
		}
		if object[name] == nil {
			// Explicitly revoked: the server is not granted.
			continue
		}
		tools, err := decodeStringList(grantPath, object[name])
		if err != nil {
			return nil, err
		}
		grants = append(grants, ServerGrant{Server: name, Tools: tools})
	}
	return grants, nil
}

func parseGateway(path string, raw any) (GatewayIntent, error) {
	intent := GatewayIntent{
		RequireAuthenticatedWorkload: true,
		UpstreamTimeoutSeconds:       DefaultUpstreamTimeoutSeconds,
	}
	fields, err := decodeObject(path, raw, []string{
		"enabled", "requireAuthenticatedWorkload", "upstreamTimeoutSeconds",
	})
	if err != nil {
		return GatewayIntent{}, err
	}
	if value, ok := fields["enabled"]; ok {
		enabled, err := decodeBool(path+".enabled", value)
		if err != nil {
			return GatewayIntent{}, err
		}
		intent.Enabled = enabled
	}
	if value, ok := fields["requireAuthenticatedWorkload"]; ok {
		required, err := decodeBool(path+".requireAuthenticatedWorkload", value)
		if err != nil {
			return GatewayIntent{}, err
		}
		intent.RequireAuthenticatedWorkload = required
	}
	if value, ok := fields["upstreamTimeoutSeconds"]; ok {
		timeout, err := decodeInt(path+".upstreamTimeoutSeconds", value)
		if err != nil {
			return GatewayIntent{}, err
		}
		intent.UpstreamTimeoutSeconds = timeout
	}
	return intent, nil
}

// validateServerShape enforces the transport-specific field combination and
// the credential-safety rules for one declared server.
func validateServerShape(server Server) error {
	path := "mcp.servers." + server.Name
	switch server.Transport {
	case TransportStreamableHTTP:
		if server.URL == "" {
			return fmt.Errorf("%s.url: is required for the %q transport", path, TransportStreamableHTTP)
		}
		if server.Command != "" || len(server.Args) > 0 {
			return fmt.Errorf("%s: command and args are only valid for the %q transport", path, TransportStdio)
		}
		if err := validateRemoteURL(path+".url", server.URL); err != nil {
			return err
		}
	case TransportStdio:
		if server.Command == "" {
			return fmt.Errorf("%s.command: is required for the %q transport", path, TransportStdio)
		}
		if server.URL != "" {
			return fmt.Errorf("%s.url: is only valid for the %q transport", path, TransportStreamableHTTP)
		}
		if err := validateLocalCommand(path+".command", server.Command); err != nil {
			return err
		}
		for index, argument := range server.Args {
			if err := validateLocalArgument(fmt.Sprintf("%s.args[%d]", path, index), argument); err != nil {
				return err
			}
		}
	}
	if server.CredentialRef != "" {
		if err := validateCredentialRef(path+".credentialRef", server.CredentialRef); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for index, tool := range server.AllowedTools {
		if !toolNamePattern.MatchString(tool) {
			return fmt.Errorf("%s.allowedTools[%d]: tool name must match %s", path, index, toolNamePattern)
		}
		if seen[tool] {
			return fmt.Errorf("%s.allowedTools[%d]: duplicate tool name", path, index)
		}
		seen[tool] = true
	}
	return nil
}

// validateRemoteURL rejects endpoints that are unsafe to route to. The URL is
// operator-supplied configuration, so callers cannot supply it at runtime.
func validateRemoteURL(path, raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s: must be a valid URL", path)
	}
	if parsed.Opaque != "" {
		return fmt.Errorf("%s: must be a hierarchical URL", path)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s: must not contain user information", path)
	}
	if parsed.Fragment != "" {
		return fmt.Errorf("%s: must not contain a fragment", path)
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(parsed.Hostname()) {
			return fmt.Errorf("%s: must use https", path)
		}
	default:
		return fmt.Errorf("%s: must use https", path)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%s: must contain a host", path)
	}
	for name, values := range parsed.Query() {
		if credentialQueryParameters[strings.ToLower(name)] {
			return fmt.Errorf("%s: must not carry credential material", path)
		}
		for _, value := range values {
			if looksLikePlaintextCredential(value) {
				return fmt.Errorf("%s: must not carry credential material", path)
			}
		}
	}
	return nil
}

// credentialQueryParameters names URL query parameters that carry credential
// material. A remote MCP endpoint is configured by the operator, so a
// credential must never travel in its URL.
var credentialQueryParameters = map[string]bool{
	"access_token":  true,
	"api_key":       true,
	"apikey":        true,
	"auth":          true,
	"authorization": true,
	"client_secret": true,
	"credential":    true,
	"credentials":   true,
	"key":           true,
	"passwd":        true,
	"password":      true,
	"refresh_token": true,
	"secret":        true,
	"sig":           true,
	"signature":     true,
	"token":         true,
}

func isLoopbackHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

// validateLocalCommand requires an absolute executable path without shell
// metacharacters. A stdio server is started by the workload, not by a shell.
func validateLocalCommand(path, command string) error {
	if !strings.HasPrefix(command, "/") {
		return fmt.Errorf("%s: must be an absolute path", path)
	}
	if strings.ContainsAny(command, " \t\n\r;|&$`><\\\"'") {
		return fmt.Errorf("%s: must not contain shell metacharacters", path)
	}
	return nil
}

// validateLocalArgument refuses arguments that carry credential material
// inline. Workload credentials are resolved from a protected reference.
func validateLocalArgument(path, argument string) error {
	if argument == "" {
		return fmt.Errorf("%s: must not be empty", path)
	}
	if strings.ContainsRune(argument, 0) {
		return fmt.Errorf("%s: must not contain a NUL byte", path)
	}
	if looksLikePlaintextCredential(argument) {
		return fmt.Errorf("%s: must not carry credential material", path)
	}
	if inlineCredentialArgument.MatchString(argument) {
		value := argument[strings.IndexByte(argument, '=')+1:]
		if !strings.HasPrefix(value, "/") {
			return fmt.Errorf("%s: must not assign credential material inline", path)
		}
	}
	return nil
}

// validateCredentialRef requires an opaque lowercase identifier that cannot
// be mistaken for credential material.
func validateCredentialRef(path, reference string) error {
	if !credentialRefPattern.MatchString(reference) {
		return fmt.Errorf("%s: must be a lowercase opaque identifier", path)
	}
	if looksLikePlaintextCredential(reference) {
		return fmt.Errorf("%s: must reference protected state, not credential material", path)
	}
	return nil
}

// looksLikePlaintextCredential reports whether a value resembles credential
// material rather than an opaque reference.
func looksLikePlaintextCredential(value string) bool {
	lowered := strings.ToLower(value)
	for _, prefix := range plaintextCredentialPrefixes {
		if strings.HasPrefix(lowered, prefix) {
			return true
		}
	}
	return false
}

// decodeObject extracts a nested JSON object and refuses any field outside
// the declared set. An empty allowed list accepts any field name.
func decodeObject(path string, raw any, allowed []string) (map[string]any, error) {
	if raw == nil {
		return nil, nil
	}
	object, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: must be a JSON object", path)
	}
	permitted := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		permitted[name] = true
	}
	for _, name := range sortedKeys(object) {
		if len(allowed) > 0 && !permitted[name] {
			return nil, fmt.Errorf("%s: unknown field %q", path, name)
		}
	}
	return object, nil
}

func decodeString(path string, raw any) (string, error) {
	if raw == nil {
		return "", fmt.Errorf("%s: must be a string", path)
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s: must be a string", path)
	}
	return value, nil
}

func decodeBool(path string, raw any) (bool, error) {
	if raw == nil {
		return false, fmt.Errorf("%s: must be a boolean", path)
	}
	value, ok := raw.(bool)
	if !ok {
		return false, fmt.Errorf("%s: must be a boolean", path)
	}
	return value, nil
}

func decodeInt(path string, raw any) (int, error) {
	if raw == nil {
		return 0, fmt.Errorf("%s: must be an integer", path)
	}
	value, ok := raw.(float64)
	if !ok {
		return 0, fmt.Errorf("%s: must be an integer", path)
	}
	if value != float64(int(value)) {
		return 0, fmt.Errorf("%s: must be an integer", path)
	}
	return int(value), nil
}

func decodeStringList(path string, raw any) ([]string, error) {
	if raw == nil {
		return nil, fmt.Errorf("%s: must be a list of strings", path)
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: must be a list of strings", path)
	}
	values := make([]string, 0, len(items))
	for index, item := range items {
		value, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d]: must be a string", path, index)
		}
		values = append(values, value)
	}
	return values, nil
}

func sortedKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
