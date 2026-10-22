package firewall

import (
	"encoding/json"
	"net/netip"
	"os"
	"os/exec"
	"slices"
	"testing"
)

func tailscaleRoutingFixture(t *testing.T) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile("testdata/tailscale-ipv4-rules.json")
	if err != nil {
		t.Fatal(err)
	}
	var rules []map[string]any
	if err := json.Unmarshal(raw, &rules); err != nil {
		t.Fatal(err)
	}
	return rules
}

func TestEgressRoutingRuleContracts(t *testing.T) {
	for _, mode := range []string{"tailscale", "kernel", "tailscale disabled", "kernel disabled", "numeric fields", "metadata", "extra", "partial", "duplicate", "priority", "mask", "mark", "table", "action", "source", "selector", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			rules := tailscaleRoutingFixture(t)
			switch mode {
			case "kernel", "kernel disabled":
				rules = append(rules[:1:1], rules[5:]...)
			case "numeric fields":
				rules[4]["table"] = 52
				rules[1]["table"] = 254
				rules[1]["fwmark"] = 524288
				rules[1]["fwmask"] = 16711680
			case "metadata":
				rules[4]["protocol"] = "static"
			case "extra":
				rules = append(rules, map[string]any{"priority": float64(100), "table": "53"})
			case "partial":
				rules = rules[1:]
			case "duplicate":
				rules[2] = rules[1]
			case "priority":
				rules[4]["priority"] = float64(5271)
			case "mask":
				rules[1]["fwmask"] = "0xffffffff"
			case "mark":
				rules[1]["fwmark"] = "0x40000"
			case "table":
				rules[4]["table"] = "53"
			case "action":
				rules[3]["action"] = "blackhole"
			case "source":
				rules[4]["src"] = "10.42.40.0/24"
			case "selector":
				rules[4]["iif"] = "aginctus-egress"
			case "unknown":
				rules[4]["future_selector"] = false
			}
			raw, err := json.Marshal(rules)
			if err != nil {
				t.Fatal(err)
			}
			err = verifyEgressRoutingRules(string(raw), mode != "tailscale disabled" && mode != "kernel disabled")
			accepted := mode == "tailscale" || mode == "kernel" || mode == "kernel disabled" || mode == "numeric fields" || mode == "metadata"
			if (err == nil) != accepted {
				t.Fatalf("accept=%v, error=%v", accepted, err)
			}
		})
	}
}

func TestEgressRoutingExcludesEveryTable(t *testing.T) {
	raw, _ := json.Marshal(tailscaleRoutingFixture(t))
	iface, blocked, err := EgressRouting(string(raw), `[
        {"dst":"default","dev":"wan0"},
        {"dst":"100.101.102.103","dev":"tailscale0","table":"52"},
        {"dst":"11.12.14.0/24","dev":"wan0","table":52},
        {"dst":"11.12.15.16","type":"local","table":"local","dev":"lo"},
        {"dst":"192.168.0.0/16","type":"throw","table":"52"},
        {"dst":"11.12.16.0/24","dev":"wan0","table":99}
    ]`, true)
	if err != nil || iface != "wan0" {
		t.Fatalf("interface=%q error=%v", iface, err)
	}
	for _, prefix := range []string{"100.101.102.103/32", "11.12.14.0/24", "11.12.15.16/32", "192.168.0.0/16", "11.12.16.0/24"} {
		if !slices.Contains(blocked, netip.MustParsePrefix(prefix)) {
			t.Fatalf("missing exclusion %s", prefix)
		}
	}
}

func TestEgressRoutingRefusesUnsafeDefaultsAndOpaqueRoutes(t *testing.T) {
	raw, _ := json.Marshal(tailscaleRoutingFixture(t))
	for _, routes := range []string{
		`[]`,
		`[{"dst":"default","dev":"wan0","table":52}]`,
		`[{"dst":"default","dev":"wan0"},{"dst":"default","dev":"wan0","table":52}]`,
		`[{"dst":"default","dev":"wan0"},{"dst":"0.0.0.0/0","dev":"wan0","table":"52"}]`,
		`[{"dst":"default","dev":"wan0"},{"dst":"default","dev":"wan1"}]`,
		`[{"dst":"default","dev":"wan0","type":"blackhole"}]`,
		`[{"dst":"default"}]`,
		`[{"dst":"default","dev":"wan0","nexthops":[]}]`,
		`[{"dst":"default","dev":"wan0","nhid":1}]`,
		`[{"dst":"default","dev":"wan0","encap":{"type":"seg6"}}]`,
		`[{"dst":"default","dev":"wan0"},{"dst":"bad","table":52}]`,
		`[{"dst":"default","dev":"wan0"},{"dst":"::/0"}]`,
	} {
		if _, _, err := EgressRouting(string(raw), routes, true); err == nil {
			t.Fatalf("accepted unsafe routes %s", routes)
		}
	}
}

// This optional audit reads host inventory only; it never invokes firewalld,
// Incus or the reconciler and cannot install or withdraw a grant.
func TestEgressRoutingReadOnlyHostInventory(t *testing.T) {
	if os.Getenv("AGINCTUS_READONLY_ROUTING_AUDIT") != "1" {
		t.Skip("optional read-only host routing audit")
	}
	rules, err := exec.Command("ip", "-j", "-4", "rule", "show").Output()
	if err != nil {
		t.Fatal(err)
	}
	routes, err := exec.Command("ip", "-j", "-4", "route", "show", "table", "all").Output()
	if err != nil {
		t.Fatal(err)
	}
	iface, blocked, err := EgressRouting(string(rules), string(routes), true)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("verified default interface %s; %d nondefault route exclusions", iface, len(blocked))
}
