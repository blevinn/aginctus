package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/blevinn/aginctus/internal/mcpschema"
)

// mcpForbiddenFields are configuration keys that would carry credential
// material into the MCP subtree.
//
// The MCP schema declares no such field, so a source that sets one is
// rejected at the source boundary before its atomic subtree is merged.
var mcpForbiddenFields = map[string]bool{
	"api_key":       true,
	"apikey":        true,
	"auth":          true,
	"authorization": true,
	"bearer":        true,
	"client_secret": true,
	"clientsecret":  true,
	"credential":    true,
	"credentials":   true,
	"env":           true,
	"header":        true,
	"headers":       true,
	"passwd":        true,
	"password":      true,
	"private_key":   true,
	"privatekey":    true,
	"secret":        true,
	"secrets":       true,
	"token":         true,
	"tokens":        true,
}

// MCP resolves the effective MCP authorization policy from the merged
// configuration.
//
// The policy denies by default: an absent MCP configuration authorizes
// nothing, and every grant must name a declared workload, a declared server
// and a tool the workload names explicitly.
func (c *Config) MCP() (mcpschema.Policy, error) {
	value, ok := c.Get("mcp")
	if !ok {
		value = nil
	}
	parsed, err := mcpschema.Parse(value)
	if err != nil {
		return mcpschema.Policy{}, err
	}
	if len(parsed.Grants) == 0 {
		// Without a grant nothing can be authorized, so no workload
		// registry is consulted. A configuration that declares no MCP
		// policy keeps its previous behavior.
		return parsed.Resolve()
	}
	workloads, err := c.workloadIdentities()
	if err != nil {
		return mcpschema.Policy{}, err
	}
	return parsed.ResolveForWorkloads(workloads)
}

// workloadIdentities returns the declared workload identities. An absent
// workload section yields an empty set, so a grant that names a workload is
// rejected instead of being resolved against an unknown identity.
func (c *Config) workloadIdentities() ([]string, error) {
	value, ok := c.Get("workloads")
	if !ok {
		return []string{}, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("workloads: must be a JSON object")
	}
	identities := make([]string, 0, len(object))
	for identity := range object {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	return identities, nil
}

// rejectMCPCredentialValues examines a prospective configuration subtree
// before any merge or assignment.
//
// It recognizes nested JSON objects and dotted JSON keys, irrespective of key
// case, and never prints the value it rejected.
func rejectMCPCredentialValues(segments []string, value any) error {
	for _, segment := range mcpForbiddenSegments(segments) {
		if mcpForbiddenFields[strings.ToLower(segment)] {
			return fmt.Errorf("MCP credential fields cannot be set through configuration")
		}
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	for _, key := range sortedConfigKeys(object) {
		child := make([]string, 0, len(segments)+2)
		child = append(child, segments...)
		child = append(child, strings.Split(key, ".")...)
		if err := rejectMCPCredentialValues(child, object[key]); err != nil {
			return err
		}
	}
	return nil
}

// mcpForbiddenSegments returns the path segments that name a field rather
// than a declared MCP object.
//
// The declared object positions are the section name, a server name, a
// capability name, a workload identity and a granted server name. A
// credential-bearing field name in any other position is refused.
func mcpForbiddenSegments(segments []string) []string {
	if len(segments) < 2 || strings.ToLower(segments[0]) != "mcp" {
		return nil
	}
	switch strings.ToLower(segments[1]) {
	case "gateway":
		return segments[2:]
	case "servers", "capabilities":
		if len(segments) <= 3 {
			return nil
		}
		return segments[3:]
	case "grants":
		if len(segments) <= 3 {
			return nil
		}
		// segments[2] is the workload identity and segments[4] is a
		// granted server name. Neither names a field.
		forbidden := append([]string{}, segments[3:4]...)
		if len(segments) > 5 {
			forbidden = append(forbidden, segments[5:]...)
		}
		return forbidden
	default:
		return nil
	}
}

func sortedConfigKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
