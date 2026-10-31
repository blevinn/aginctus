package mcpschema

import (
	"fmt"
	"sort"
)

// EffectiveServer is the effective tool set one workload holds for one
// server.
type EffectiveServer struct {
	// Server is the declared server name.
	Server string `json:"server"`
	// Transport is the server's transport.
	Transport Transport `json:"transport"`
	// Tools lists the authorized tool names, ordered.
	Tools []string `json:"tools"`
}

// WorkloadPolicy is the effective policy for one workload.
type WorkloadPolicy struct {
	// Workload is the workload identity the policy belongs to.
	Workload string
	// Capability is the declared capability profile name.
	Capability string
	// Protocol is the model-facing protocol of the capability profile.
	Protocol Protocol
	// Requires lists the required capability tokens, ordered.
	Requires []string
	// Servers lists the servers the workload holds effective grants for,
	// ordered by server name.
	Servers []EffectiveServer
}

// Policy is the resolved effective MCP authorization policy.
//
// A policy is immutable. Every accessor returns a defensive copy so a caller
// cannot mutate shared state, and the ordering of every collection is
// deterministic.
//
// A policy states declared authorization only. It is not proof that any
// request was authorized: binding a request to an authenticated workload
// identity and re-checking every invocation is runtime enforcement.
type Policy struct {
	servers   []Server
	workloads []WorkloadPolicy
	gateway   GatewayIntent
}

// Servers returns the declared servers, ordered by name.
func (p Policy) Servers() []Server {
	servers := make([]Server, 0, len(p.servers))
	for _, server := range p.servers {
		servers = append(servers, cloneServer(server))
	}
	return servers
}

// Server returns one declared server by name.
func (p Policy) Server(name string) (Server, bool) {
	for _, server := range p.servers {
		if server.Name == name {
			return cloneServer(server), true
		}
	}
	return Server{}, false
}

// Workloads returns the effective per-workload policies, ordered by workload
// identity.
func (p Policy) Workloads() []WorkloadPolicy {
	workloads := make([]WorkloadPolicy, 0, len(p.workloads))
	for _, workload := range p.workloads {
		workloads = append(workloads, cloneWorkloadPolicy(workload))
	}
	return workloads
}

// Workload returns one workload's effective policy.
func (p Policy) Workload(id string) (WorkloadPolicy, bool) {
	for _, workload := range p.workloads {
		if workload.Workload == id {
			return cloneWorkloadPolicy(workload), true
		}
	}
	return WorkloadPolicy{}, false
}

// Gateway returns the declared central gateway MCP intent.
func (p Policy) Gateway() GatewayIntent {
	return p.gateway
}

// Allows reports whether the declared policy authorizes one tool for one
// workload and server.
//
// The answer is the intersection of the server's own tool restriction and
// the workload's explicit tool grant. A server grant never authorizes a tool
// the workload did not name.
func (p Policy) Allows(workload, server, tool string) bool {
	effective, ok := p.Workload(workload)
	if !ok {
		return false
	}
	for _, granted := range effective.Servers {
		if granted.Server != server {
			continue
		}
		for _, candidate := range granted.Tools {
			if candidate == tool {
				return true
			}
		}
	}
	return false
}

// Validate reports whether the parsed configuration is internally consistent.
//
// Validation never resolves credential references and never reads protected
// credential contents. It only inspects declared structure.
func (c Config) Validate() error {
	if !c.Gateway.RequireAuthenticatedWorkload {
		return fmt.Errorf(
			"mcp.gateway.requireAuthenticatedWorkload: must remain enabled, the gateway must bind requests to an authenticated workload identity",
		)
	}
	if c.Gateway.UpstreamTimeoutSeconds < 1 || c.Gateway.UpstreamTimeoutSeconds > MaxUpstreamTimeoutSeconds {
		return fmt.Errorf(
			"mcp.gateway.upstreamTimeoutSeconds: must be between 1 and %d",
			MaxUpstreamTimeoutSeconds,
		)
	}

	profiles := map[string]CapabilityProfile{}
	for _, profile := range c.Capabilities {
		if err := validateCapabilityProfile(profile); err != nil {
			return err
		}
		profiles[profile.Name] = profile
	}

	servers := map[string]Server{}
	remote := 0
	for _, server := range c.Servers {
		if err := validateServerShape(server); err != nil {
			return err
		}
		if _, duplicate := servers[server.Name]; duplicate {
			return fmt.Errorf("mcp.servers.%s: duplicate server name", server.Name)
		}
		servers[server.Name] = server
		if server.Transport == TransportStreamableHTTP {
			remote++
		}
	}

	// Remote servers exist to be routed by the central gateway, and the
	// central gateway only exists to route remote servers. Declaring one
	// side without the other is a contradiction and fails closed.
	if c.Gateway.Enabled && remote == 0 {
		return fmt.Errorf(
			"mcp.gateway.enabled: requires at least one %q server to route",
			TransportStreamableHTTP,
		)
	}
	if !c.Gateway.Enabled && remote > 0 {
		return fmt.Errorf(
			"mcp.gateway.enabled: must be enabled to route the declared %q servers",
			TransportStreamableHTTP,
		)
	}

	for _, grant := range c.Grants {
		if err := validateGrant(grant, profiles, servers); err != nil {
			return err
		}
	}
	return nil
}

// Resolve validates the configuration and returns the deterministic
// effective policy.
//
// Grant workloads are not checked against a workload registry here. Use
// ResolveForWorkloads when the declared workload identities are known.
func (c Config) Resolve() (Policy, error) {
	return c.ResolveForWorkloads(nil)
}

// ResolveForWorkloads validates the configuration and resolves the policy
// against a set of declared workload identities.
//
// A grant for a workload that is not declared is rejected. A nil set skips
// that check, which is only appropriate when no workload registry is
// available to the caller.
func (c Config) ResolveForWorkloads(knownWorkloads []string) (Policy, error) {
	if err := c.Validate(); err != nil {
		return Policy{}, err
	}
	if knownWorkloads != nil {
		known := make(map[string]bool, len(knownWorkloads))
		for _, workload := range knownWorkloads {
			known[workload] = true
		}
		for _, grant := range c.Grants {
			if !known[grant.Workload] {
				return Policy{}, fmt.Errorf(
					"mcp.grants.%s: unknown workload",
					grant.Workload,
				)
			}
		}
	}

	policy := Policy{gateway: c.Gateway}
	for _, server := range c.Servers {
		policy.servers = append(policy.servers, cloneServer(server))
	}
	sort.Slice(policy.servers, func(i, j int) bool {
		return policy.servers[i].Name < policy.servers[j].Name
	})

	profiles := map[string]CapabilityProfile{}
	for _, profile := range c.Capabilities {
		profiles[profile.Name] = profile
	}
	servers := map[string]Server{}
	for _, server := range c.Servers {
		servers[server.Name] = server
	}

	for _, grant := range c.Grants {
		workload := WorkloadPolicy{
			Workload:   grant.Workload,
			Capability: grant.Capability,
		}
		if profile, ok := profiles[grant.Capability]; ok {
			workload.Protocol = profile.Protocol
			workload.Requires = append([]string(nil), profile.Requires...)
			sort.Strings(workload.Requires)
		}
		for _, serverGrant := range grant.Servers {
			if len(serverGrant.Tools) == 0 {
				// An empty tool list grants no access. The server stays
				// unreachable for this workload.
				continue
			}
			server := servers[serverGrant.Server]
			effective := EffectiveServer{
				Server:    serverGrant.Server,
				Transport: server.Transport,
				Tools:     intersectTools(serverGrant.Tools, server),
			}
			workload.Servers = append(workload.Servers, effective)
		}
		sort.Slice(workload.Servers, func(i, j int) bool {
			return workload.Servers[i].Server < workload.Servers[j].Server
		})
		policy.workloads = append(policy.workloads, workload)
	}
	sort.Slice(policy.workloads, func(i, j int) bool {
		return policy.workloads[i].Workload < policy.workloads[j].Workload
	})
	return policy, nil
}

// intersectTools returns the authorized tool names for one grant. The server
// restriction and the workload grant are both applied; neither can widen the
// other.
func intersectTools(granted []string, server Server) []string {
	allowed := map[string]bool{}
	if server.RestrictsTools() {
		for _, tool := range server.AllowedTools {
			allowed[tool] = true
		}
	}
	tools := make([]string, 0, len(granted))
	for _, tool := range granted {
		if server.RestrictsTools() && !allowed[tool] {
			continue
		}
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	return tools
}

func validateCapabilityProfile(profile CapabilityProfile) error {
	path := "mcp.capabilities." + profile.Name
	switch profile.Protocol {
	case ProtocolResponses, ProtocolChatCompletions:
	default:
		return fmt.Errorf(
			"%s.protocol: unsupported protocol %q, want %q or %q",
			path, profile.Protocol, ProtocolResponses, ProtocolChatCompletions,
		)
	}
	seen := map[string]bool{}
	for index, capability := range profile.Requires {
		switch capability {
		case CapabilityMCPTools, CapabilityProviderHostedTools:
		default:
			return fmt.Errorf(
				"%s.requires[%d]: unsupported MCP capability %q, it requires policy that is not implemented",
				path, index, capability,
			)
		}
		if seen[capability] {
			return fmt.Errorf("%s.requires[%d]: duplicate capability", path, index)
		}
		seen[capability] = true
	}
	return nil
}

func validateGrant(
	grant Grant,
	profiles map[string]CapabilityProfile,
	servers map[string]Server,
) error {
	path := "mcp.grants." + grant.Workload
	granted := 0
	for _, serverGrant := range grant.Servers {
		if len(serverGrant.Tools) > 0 {
			granted++
		}
	}

	if grant.Capability == "" {
		if granted > 0 {
			return fmt.Errorf("%s.capability: is required when the workload holds MCP server grants", path)
		}
		return nil
	}
	profile, ok := profiles[grant.Capability]
	if !ok {
		return fmt.Errorf("%s.capability: unknown capability profile %q", path, grant.Capability)
	}
	if granted > 0 && !contains(profile.Requires, CapabilityMCPTools) {
		return fmt.Errorf(
			"%s.capability: capability profile %q must require %q when the workload holds MCP server grants",
			path, grant.Capability, CapabilityMCPTools,
		)
	}

	for _, serverGrant := range grant.Servers {
		grantPath := path + ".servers." + serverGrant.Server
		server, ok := servers[serverGrant.Server]
		if !ok {
			return fmt.Errorf("%s: unknown MCP server", grantPath)
		}
		if len(serverGrant.Tools) == 0 {
			continue
		}
		seen := map[string]bool{}
		for index, tool := range serverGrant.Tools {
			if !toolNamePattern.MatchString(tool) {
				return fmt.Errorf("%s[%d]: tool name must match %s", grantPath, index, toolNamePattern)
			}
			if seen[tool] {
				return fmt.Errorf("%s[%d]: duplicate tool grant", grantPath, index)
			}
			seen[tool] = true
			if server.RestrictsTools() && !contains(server.AllowedTools, tool) {
				return fmt.Errorf(
					"%s[%d]: tool grant is outside the server tool allowlist",
					grantPath, index,
				)
			}
		}
	}
	return nil
}

func contains(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func cloneServer(server Server) Server {
	clone := server
	clone.Args = append([]string(nil), server.Args...)
	if server.AllowedTools != nil {
		clone.AllowedTools = append([]string{}, server.AllowedTools...)
	}
	return clone
}

func cloneWorkloadPolicy(workload WorkloadPolicy) WorkloadPolicy {
	clone := workload
	clone.Requires = append([]string(nil), workload.Requires...)
	servers := make([]EffectiveServer, 0, len(workload.Servers))
	for _, server := range workload.Servers {
		server.Tools = append([]string(nil), server.Tools...)
		servers = append(servers, server)
	}
	clone.Servers = servers
	return clone
}
