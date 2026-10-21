package firewall

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"sort"
)

const EgressZone = "aginctus-egress"
const EgressOwner = "aginctus:egress:v1; NixOS structure, Aginctus runtime grants"
const EgressHTTPS = "aginctus-egress-https"
const EgressDNS = "aginctus-egress-host"

var uplinkZoneName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// Settings includes unknown properties so verification fails closed on new fields.
type Settings map[string]any

type EgressGrant struct {
	Source string
	Uplink string
	HTTPS  []string
	DNS    []string
}

// EgressObjects is the ownership contract shared with nix/egress-firewall.nix.
// Only grant rich_rules and the HTTPS egress_zones may change at runtime.
func EgressObjects(bridge string) map[string]Settings {
	base := func(target string) Settings { return Settings{"description": EgressOwner, "target": target} }
	zone := base("DROP")
	zone["interfaces"] = []string{bridge}
	zone["ingress_priority"], zone["egress_priority"] = float64(-32768), float64(-32768)
	objects := map[string]Settings{EgressZone: zone}
	for _, p := range []struct {
		name, ingress, egress, target string
		priority                      int
	}{
		{"aginctus-egress-out", EgressZone, "ANY", "DROP", -500},
		{"aginctus-egress-in", "ANY", EgressZone, "DROP", -500},
		{"aginctus-egress-input", EgressZone, "HOST", "DROP", -500},
		{EgressHTTPS, EgressZone, "", "CONTINUE", -1000},
		{EgressDNS, EgressZone, "HOST", "CONTINUE", -1000},
	} {
		s := base(p.target)
		s["ingress_zones"], s["egress_zones"], s["priority"] = []string{p.ingress}, []string{p.egress}, float64(p.priority)
		if p.egress == "" {
			s["egress_zones"] = []string{}
		}
		objects[p.name] = s
	}
	return objects
}

// VerifySettings allows only documented neutral defaults beyond the contract.
func VerifySettings(got, want Settings) error {
	defaults := Settings{
		"short": "", "version": "", "services": []string{}, "ports": []string{},
		"protocols": []string{}, "source_ports": []string{}, "forward_ports": []string{},
		"icmp_blocks": []string{}, "icmp_block_inversion": false, "masquerade": false,
		"forward": false, "sources": []string{}, "rules_str": []string{}, "rich_rules": []string{},
		"ingress_priority": float64(0), "egress_priority": float64(0), "disable": false,
		"helpers": []string{},
	}
	for key, value := range want {
		if !settingsEqual(got[key], value) {
			return fmt.Errorf("firewalld setting %s differs from ownership contract", key)
		}
	}
	for key, value := range got {
		if _, ok := want[key]; ok {
			continue
		}
		neutral, ok := defaults[key]
		if !ok || !settingsEqual(value, neutral) {
			return fmt.Errorf("unexpected firewalld setting %s", key)
		}
	}
	return nil
}

func settingsEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	if string(x) == string(y) {
		return true
	}
	var ax, bx []string
	if json.Unmarshal(x, &ax) == nil && json.Unmarshal(y, &bx) == nil {
		sort.Strings(ax)
		sort.Strings(bx)
		return slices.Equal(ax, bx)
	}
	return false
}

// EgressRules generates IPv4-only grants excluding every nondefault host route.
func EgressRules(source, bridgeCIDR, uplink string, routes []netip.Prefix) (EgressGrant, error) {
	grant := EgressGrant{Uplink: uplink}
	ip, err := netip.ParsePrefix(source)
	bridge, bridgeErr := netip.ParsePrefix(bridgeCIDR)
	if err != nil || bridgeErr != nil || !ip.Addr().Is4() || !bridge.Addr().Is4() ||
		bridge.Bits() < 16 || bridge.Bits() > 30 || ip.Bits() != bridge.Bits() || !bridge.Masked().Contains(ip.Addr()) || !bridge.Masked().Contains(ip.Addr().Next()) || ip.Addr() == bridge.Addr() ||
		ip.Addr() == bridge.Masked().Addr() || uplink == "" || uplink == "ANY" || uplink == "HOST" ||
		uplink == "trusted" || uplink == EgressZone || !uplinkZoneName.MatchString(uplink) {
		return grant, fmt.Errorf("invalid egress source, bridge or uplink")
	}
	grant.Source = ip.Addr().String()
	blocked := []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4"}
	for _, route := range routes {
		if !route.IsValid() || !route.Addr().Is4() {
			return grant, fmt.Errorf("invalid host IPv4 route")
		}
		if route.Bits() != 0 {
			blocked = append(blocked, route.Masked().String())
		}
	}
	sort.Strings(blocked)
	blocked = slices.Compact(blocked)
	for _, dest := range blocked {
		grant.HTTPS = append(grant.HTTPS, fmt.Sprintf(`rule priority="-300" family="ipv4" source address="%s/32" destination address="%s" drop`, ip.Addr(), dest))
	}
	grant.HTTPS = append(grant.HTTPS, fmt.Sprintf(`rule priority="-200" family="ipv4" source address="%s/32" port port="443" protocol="tcp" accept`, ip.Addr()))
	grant.DNS, err = FirewalldRules(&Policy{Interface: "aginctus-egress", Address: bridge.Addr().String(), Subnet: bridge.Masked().String()})
	return grant, err
}
