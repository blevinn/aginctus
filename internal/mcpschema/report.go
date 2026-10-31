package mcpschema

import (
	"encoding/json"
	"fmt"
)

// ServerReport is the redacted view of one declared server.
type ServerReport struct {
	// Name is the declared server name.
	Name string `json:"name"`
	// Transport is the server's transport.
	Transport Transport `json:"transport"`
	// Endpoint is the remote Streamable HTTP endpoint. Empty for a
	// workload-local stdio server.
	Endpoint string `json:"endpoint"`
	// Command is the absolute executable path of a workload-local stdio
	// server. Empty for a remote server.
	Command string `json:"command"`
	// HasCredentialReference reports whether the server declares a
	// credential reference. The reference itself is never reported.
	HasCredentialReference bool `json:"hasCredentialReference"`
	// AllowedTools is the declared server tool surface. Null means the
	// server declares no server-level restriction, which is different from
	// an empty list, which permits no tool at all.
	AllowedTools []string `json:"allowedTools"`
}

// WorkloadReport is the redacted view of one workload's effective policy.
type WorkloadReport struct {
	// Workload is the workload identity the policy belongs to.
	Workload string `json:"workload"`
	// Capability is the declared capability profile name.
	Capability string `json:"capability"`
	// Protocol is the model-facing protocol of the capability profile.
	Protocol Protocol `json:"protocol"`
	// Requires lists the required capability tokens.
	Requires []string `json:"requires"`
	// Servers lists the servers the workload holds effective grants for.
	Servers []EffectiveServer `json:"servers"`
}

// GatewayReport is the redacted view of the declared gateway intent.
type GatewayReport struct {
	// Enabled reports whether the central gateway MCP endpoint is intended
	// to be exposed.
	Enabled bool `json:"enabled"`
	// RequireAuthenticatedWorkload reports whether the gateway must bind
	// requests to an authenticated workload identity.
	RequireAuthenticatedWorkload bool `json:"requireAuthenticatedWorkload"`
	// UpstreamTimeoutSeconds bounds one upstream MCP call.
	UpstreamTimeoutSeconds int `json:"upstreamTimeoutSeconds"`
}

// Report is a deterministic, redacted view of an effective policy.
//
// The report never contains credential material and never contains a
// credential reference. It is safe to log, to serialize and to compare in a
// test.
//
// The report states declared authorization. It is not proof that any request
// was authorized.
type Report struct {
	// Servers lists the declared servers, ordered by name.
	Servers []ServerReport `json:"servers"`
	// Workloads lists the effective per-workload policies, ordered by
	// workload identity.
	Workloads []WorkloadReport `json:"workloads"`
	// Gateway states the declared central gateway MCP intent.
	Gateway GatewayReport `json:"gateway"`
}

// Report returns the redacted, deterministic view of the policy.
func (p Policy) Report() Report {
	report := Report{
		Servers:   []ServerReport{},
		Workloads: []WorkloadReport{},
		Gateway: GatewayReport{
			Enabled:                      p.gateway.Enabled,
			RequireAuthenticatedWorkload: p.gateway.RequireAuthenticatedWorkload,
			UpstreamTimeoutSeconds:       p.gateway.UpstreamTimeoutSeconds,
		},
	}
	for _, server := range p.servers {
		entry := ServerReport{
			Name:                   server.Name,
			Transport:              server.Transport,
			HasCredentialReference: server.CredentialRef != "",
		}
		switch server.Transport {
		case TransportStreamableHTTP:
			entry.Endpoint = server.URL
		case TransportStdio:
			entry.Command = server.Command
		}
		if server.AllowedTools != nil {
			entry.AllowedTools = append([]string{}, server.AllowedTools...)
		}
		report.Servers = append(report.Servers, entry)
	}
	for _, workload := range p.workloads {
		entry := WorkloadReport{
			Workload:   workload.Workload,
			Capability: workload.Capability,
			Protocol:   workload.Protocol,
			Requires:   append([]string{}, workload.Requires...),
			Servers:    []EffectiveServer{},
		}
		for _, server := range workload.Servers {
			entry.Servers = append(entry.Servers, EffectiveServer{
				Server:    server.Server,
				Transport: server.Transport,
				Tools:     append([]string{}, server.Tools...),
			})
		}
		report.Workloads = append(report.Workloads, entry)
	}
	return report
}

// JSON returns the deterministic JSON encoding of the report.
func (r Report) JSON() ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode MCP policy report: %w", err)
	}
	return append(data, '\n'), nil
}
