package gateway

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
)

// loadFromFile parses a JSON document into an effective config.Loader output.
// It deliberately writes the document to a temp file and reloads it through
// the production loader so the literal-dot path resolution exercised by
// FromConfig matches a real NixOS-rendered /etc/aginctus/config.json.
func loadFromFile(t *testing.T, body string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	loader := &config.Loader{
		SystemPath: filepath.Join(t.TempDir(), "missing-system.json"),
		UserPath:   filepath.Join(t.TempDir(), "missing-user.json"),
	}
	cfg, err := loader.Load(config.Options{ConfigurationFile: path})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

func TestFromConfigAcceptsNestedEgressDHCPObj(t *testing.T) {
	body := `{
	  "gateway": {
	    "id": "local",
	    "egress": {
	      "ipv4": {"address": "10.42.40.25/24"},
	      "network": {
	        "name": "aginctus-egress",
	        "ipv4": {
	          "address": "10.42.40.1/24",
	          "dhcp": {"enabled": true, "ranges": "10.42.40.100-10.42.40.199"}
	        }
	      }
	    }
	  }
	}`
	spec, err := FromConfig(loadFromFile(t, body))
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}
	if spec.EgressDHCPDisabled {
		t.Fatalf("EgressDHCPDisabled = true, want false (nested object explicit enabled=true)")
	}
	if spec.EgressDHCPRanges != "10.42.40.100-10.42.40.199" {
		t.Fatalf("EgressDHCPRanges = %q, want 10.42.40.100-10.42.40.199", spec.EgressDHCPRanges)
	}
	if spec.EgressBridgeIPv4CIDR != "10.42.40.1/24" {
		t.Fatalf("EgressBridgeIPv4CIDR = %q, want 10.42.40.1/24", spec.EgressBridgeIPv4CIDR)
	}
	if spec.EgressIPv4Address != "10.42.40.25/24" {
		t.Fatalf("EgressIPv4Address = %q, want 10.42.40.25/24", spec.EgressIPv4Address)
	}
}

func TestFromConfigAcceptsLegacyFlatEgressDHCPBool(t *testing.T) {
	body := `{
	  "gateway": {
	    "id": "local",
	    "egress": {
	      "ipv4": {"address": "10.42.40.25/24"},
	      "network": {
	        "name": "aginctus-egress",
	        "ipv4": {
	          "address": "10.42.40.1/24",
	          "dhcp": true
	        }
	      }
	    }
	  }
	}`
	spec, err := FromConfig(loadFromFile(t, body))
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}
	if spec.EgressDHCPDisabled {
		t.Fatalf("EgressDHCPDisabled = true, want false (legacy flat dhcp=true)")
	}
	if spec.EgressDHCPRanges != "" {
		t.Fatalf("EgressDHCPRanges = %q, want empty (legacy flat dhcp=true)", spec.EgressDHCPRanges)
	}
}

func TestFromConfigRejectsLiteralDotSiblingUnderBoolDHCP(t *testing.T) {
	// The dot-splitting loader cannot index a literal-dot key once its parent
	// resolves to a non-map value. The user-facing JSON shape
	// { "dhcp": true, "dhcp.ranges": "..." } survives the JSON encoder but
	// never reaches FromConfig ranges; the spec therefore remains consistent
	// and the orchestration plan omits ipv4.dhcp.ranges, not because the
	// bridge is unrestricted but because the path is unresolvable.
	body := `{
	  "gateway": {
	    "id": "local",
	    "egress": {
	      "ipv4": {"address": "10.42.40.25/24"},
	      "network": {
	        "name": "aginctus-egress",
	        "ipv4": {
	          "address": "10.42.40.1/24",
	          "dhcp": true,
	          "dhcp.ranges": "10.42.40.100-10.42.40.199"
	        }
	      }
	    }
	  }
	}`
	spec, err := FromConfig(loadFromFile(t, body))
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}
	if spec.EgressDHCPDisabled {
		t.Fatalf("EgressDHCPDisabled = true, want false")
	}
	if spec.EgressDHCPRanges != "" {
		t.Fatalf("EgressDHCPRanges = %q, want empty (literal-dot sibling cannot be resolved by the dot-splitting loader)", spec.EgressDHCPRanges)
	}
}

func TestFromConfigRejectsUnsupportedEgressDHCPType(t *testing.T) {
	body := `{
	  "gateway": {
	    "id": "local",
	    "egress": {
	      "network": {
	        "ipv4": {"dhcp": 42}
	      }
	    }
	  }
	}`
	if _, err := FromConfig(loadFromFile(t, body)); err == nil {
		t.Fatal("FromConfig accepted unsupported ipv4.dhcp type")
	}
}