package firewall

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"reflect"
	"strconv"
)

// EgressRouting verifies the supported RPDB layouts and returns the sole main-
// table default interface plus every nondefault destination from every table.
// The caller must verify that interface's zone and exclude both guest bridges.
func EgressRouting(rawRules, rawRoutes string, allowTailscaleRouting bool) (string, []netip.Prefix, error) {
	if err := verifyEgressRoutingRules(rawRules, allowTailscaleRouting); err != nil {
		return "", nil, err
	}
	var routes []struct {
		Dst      string          `json:"dst"`
		Dev      string          `json:"dev"`
		Type     string          `json:"type"`
		Table    any             `json:"table"`
		Nexthops json.RawMessage `json:"nexthops"`
		NHID     json.RawMessage `json:"nhid"`
		Encap    json.RawMessage `json:"encap"`
	}
	if err := json.Unmarshal([]byte(rawRoutes), &routes); err != nil {
		return "", nil, err
	}
	var blocked []netip.Prefix
	iface := ""
	for _, route := range routes {
		if len(route.Nexthops) != 0 || len(route.NHID) != 0 || len(route.Encap) != 0 {
			return "", nil, fmt.Errorf("multipath or encapsulated routes are unsupported")
		}
		dst := route.Dst
		if dst == "default" {
			dst = "0.0.0.0/0"
		}
		prefix, err := netip.ParsePrefix(dst)
		if err != nil {
			if ip, e := netip.ParseAddr(dst); e == nil && ip.Is4() {
				prefix = netip.PrefixFrom(ip, 32)
			} else {
				return "", nil, fmt.Errorf("opaque IPv4 route destination %q", route.Dst)
			}
		}
		if !prefix.Addr().Is4() {
			return "", nil, fmt.Errorf("non-IPv4 route in IPv4 inventory")
		}
		if prefix.Bits() == 0 {
			// Omitted table means main in `ip route show table all`. Other
			// defaults, including exit-node routes in table 52, are refused.
			if (route.Table != nil && routingTable(route.Table) != "main") || (route.Type != "" && route.Type != "unicast") || route.Dev == "" || iface != "" {
				return "", nil, fmt.Errorf("exactly one unicast main-table IPv4 default route required")
			}
			iface = route.Dev
			continue
		}
		blocked = append(blocked, prefix.Masked())
	}
	if iface == "" {
		return "", nil, fmt.Errorf("main-table IPv4 default route required")
	}
	return iface, blocked, nil
}

func routingTable(value any) string {
	text := fmt.Sprint(value)
	switch text {
	case "255", "local":
		return "local"
	case "254", "main":
		return "main"
	case "253", "default":
		return "default"
	default:
		return text
	}
}

// Only the kernel layout or the complete Tailscale bypass/table-52 layout is
// supported; extra selectors and partially installed VPN rules fail closed.
func verifyEgressRoutingRules(raw string, allowTailscaleRouting bool) error {
	var rules []map[string]any
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return err
	}
	want := []map[string]any{
		{"priority": float64(0), "src": "all", "table": "local"},
		{"priority": float64(32766), "src": "all", "table": "main"},
		{"priority": float64(32767), "src": "all", "table": "default"},
	}
	if allowTailscaleRouting && len(rules) == 7 {
		vpn := []map[string]any{
			{"priority": float64(5210), "src": "all", "fwmark": "0x80000", "fwmask": "0xff0000", "table": "main"},
			{"priority": float64(5230), "src": "all", "fwmark": "0x80000", "fwmask": "0xff0000", "table": "default"},
			{"priority": float64(5250), "src": "all", "fwmark": "0x80000", "fwmask": "0xff0000", "action": "unreachable"},
			{"priority": float64(5270), "src": "all", "table": "52"},
		}
		want = append(append(want[:1:1], vpn...), want[1:]...)
	}
	if len(rules) != len(want) {
		return fmt.Errorf("unsupported IPv4 policy-routing layout")
	}
	for i, rule := range rules {
		delete(rule, "protocol") // Installation metadata, not a packet selector.
		if table, ok := rule["table"]; ok {
			rule["table"] = routingTable(table)
		}
		for _, field := range []string{"fwmark", "fwmask"} {
			if value, ok := rule[field]; ok {
				text := fmt.Sprint(value)
				if number, ok := value.(float64); ok {
					text = strconv.FormatFloat(number, 'f', -1, 64)
				}
				base := 10
				if len(text) > 2 && text[:2] == "0x" {
					base, text = 16, text[2:]
				}
				n, err := strconv.ParseUint(text, base, 32)
				if err != nil {
					return fmt.Errorf("invalid IPv4 rule %s", field)
				}
				rule[field] = fmt.Sprintf("0x%x", n)
			}
		}
		if !reflect.DeepEqual(rule, want[i]) {
			return fmt.Errorf("unsupported IPv4 routing rule at index %d", i)
		}
	}
	return nil
}
