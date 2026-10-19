package gateway

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
)

// Use the production file loader so literal JSON keys retain their input shape.
func loadFromFile(t *testing.T, body string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	loader := &config.Loader{}
	cfg, err := loader.Load(config.Options{ConfigurationFile: path})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

func TestFromConfigEgressDHCP(t *testing.T) {
	const ranges = "10.42.40.100-10.42.40.199"
	tests := []struct {
		name     string
		members  string
		disabled bool
		ranges   string
		wantErr  string
	}{
		{name: "omitted"},
		{name: "legacy enabled", members: `,"dhcp":true`},
		{name: "legacy disabled", members: `,"dhcp":false`, disabled: true},
		{name: "object enabled", members: `,"dhcp":{"enabled":true}`},
		{name: "object disabled", members: `,"dhcp":{"enabled":false}`, disabled: true},
		{name: "restricted pool", members: `,"dhcp":{"enabled":true,"ranges":"` + ranges + `"}`, ranges: ranges},
		{name: "empty ranges", members: `,"dhcp":{"enabled":true,"ranges":""}`},
		{name: "disabled empty ranges", members: `,"dhcp":{"enabled":false,"ranges":""}`, disabled: true},
		{name: "string", members: `,"dhcp":"sensitive-input"`, wantErr: "must be a boolean or an object"},
		{name: "number", members: `,"dhcp":42`, wantErr: "must be a boolean or an object"},
		{name: "null", members: `,"dhcp":null`, wantErr: "must be a boolean or an object"},
		{name: "array", members: `,"dhcp":[]`, wantErr: "must be a boolean or an object"},
		{name: "empty object", members: `,"dhcp":{}`, wantErr: "enabled must be a boolean"},
		{name: "missing enabled", members: `,"dhcp":{"ranges":"` + ranges + `"}`, wantErr: "enabled must be a boolean"},
		{name: "string enabled", members: `,"dhcp":{"enabled":"false"}`, wantErr: "enabled must be a boolean"},
		{name: "number enabled", members: `,"dhcp":{"enabled":0}`, wantErr: "enabled must be a boolean"},
		{name: "null enabled", members: `,"dhcp":{"enabled":null}`, wantErr: "enabled must be a boolean"},
		{name: "object enabled value", members: `,"dhcp":{"enabled":{}}`, wantErr: "enabled must be a boolean"},
		{name: "array enabled", members: `,"dhcp":{"enabled":[]}`, wantErr: "enabled must be a boolean"},
		{name: "boolean ranges", members: `,"dhcp":{"enabled":true,"ranges":false}`, wantErr: "ranges must be a string"},
		{name: "number ranges", members: `,"dhcp":{"enabled":true,"ranges":42}`, wantErr: "ranges must be a string"},
		{name: "null ranges", members: `,"dhcp":{"enabled":true,"ranges":null}`, wantErr: "ranges must be a string"},
		{name: "object ranges", members: `,"dhcp":{"enabled":true,"ranges":{}}`, wantErr: "ranges must be a string"},
		{name: "array ranges", members: `,"dhcp":{"enabled":true,"ranges":[]}`, wantErr: "ranges must be a string"},
		{name: "misspelled ranges", members: `,"dhcp":{"enabled":true,"range":"sensitive-input"}`, wantErr: "only enabled and ranges"},
		{name: "unknown field", members: `,"dhcp":{"enabled":true,"sensitive-input":"sensitive-input"}`, wantErr: "only enabled and ranges"},
		{name: "nested dotted field", members: `,"dhcp":{"enabled":true,"ranges.start":"sensitive-input"}`, wantErr: "only enabled and ranges"},
		{name: "disabled restricted pool", members: `,"dhcp":{"enabled":false,"ranges":"` + ranges + `"}`, wantErr: "require enabled DHCP"},
		{name: "invalid range syntax", members: `,"dhcp":{"enabled":true,"ranges":"sensitive-input"}`, wantErr: "gateway egress DHCP ranges"},
		{name: "bool dotted collision", members: `,"dhcp":true,"dhcp.ranges":"sensitive-input"`, wantErr: "literal dotted"},
		{name: "disabled dotted collision", members: `,"dhcp":false,"dhcp.ranges":"sensitive-input"`, wantErr: "literal dotted"},
		{name: "object dotted collision", members: `,"dhcp":{"enabled":true},"dhcp.ranges":"sensitive-input"`, wantErr: "literal dotted"},
		{name: "dotted without dhcp", members: `,"dhcp.ranges":"sensitive-input"`, wantErr: "literal dotted"},
		{name: "dotted enabled", members: `,"dhcp.enabled":false`, wantErr: "literal dotted"},
		{name: "dotted typo", members: `,"dhcp.range":"sensitive-input"`, wantErr: "literal dotted"},
		{name: "unrelated dotted key", members: `,"other.setting":"unchanged"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"gateway":{"egress":{
				"ipv4":{"address":"10.42.40.25/24"},
				"network":{"ipv4":{"address":"10.42.40.1/24"%s}}
			}}}`, tt.members)
			spec, err := FromConfig(loadFromFile(t, body))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("FromConfig error = %v, want %q", err, tt.wantErr)
				}
				if strings.Contains(err.Error(), "sensitive-input") {
					t.Fatalf("error discloses input: %v", err)
				}
				if spec != (Spec{}) {
					t.Fatalf("invalid policy returned a usable spec: %+v", spec)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if spec.EgressDHCPDisabled != tt.disabled || spec.EgressDHCPRanges != tt.ranges {
				t.Fatalf("DHCP disabled/ranges = %v/%q, want %v/%q", spec.EgressDHCPDisabled, spec.EgressDHCPRanges, tt.disabled, tt.ranges)
			}
		})
	}
}

func TestFromConfigRejectsDottedEgressDHCPAncestors(t *testing.T) {
	for _, body := range []string{
		`{"gateway.egress.network.ipv4.dhcp":false}`,
		`{"gateway.egress":{"network":{"ipv4":{"dhcp":false}}}}`,
		`{"gateway":{"egress.network.ipv4.dhcp.ranges":"sensitive-input"}}`,
		`{"gateway":{"egress":{"network.ipv4":{"dhcp":false}}}}`,
		`{"gateway":{"egress":{"network":{"ipv4.dhcp":false}}}}`,
	} {
		t.Run(body, func(t *testing.T) {
			_, err := FromConfig(loadFromFile(t, body))
			if err == nil || !strings.Contains(err.Error(), "literal dotted") {
				t.Fatalf("FromConfig error = %v, want literal dotted key rejection", err)
			}
		})
	}
}
