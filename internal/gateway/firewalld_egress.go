package gateway

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
)

// EgressFirewalldPlan describes a directional HTTPS exception. It is advisory:
// applying it requires an explicit firewall lifecycle integration; gateway
// deployment does not alter firewalld policy.
type EgressFirewalldPlan struct {
	Zone       string   `json:"zone"`
	Interface  string   `json:"interface"`
	UplinkZone string   `json:"uplink_zone"`
	Policy     string   `json:"policy"`
	Source     string   `json:"source"`
	RichRules  []string `json:"rich_rules"`
	HTTPS      bool     `json:"https"`
	Target     string   `json:"target"`
	Warning    string   `json:"warning"`
}

var firewalldZoneIdentifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// PlanEgressFirewalld is read-only. It refuses unspecified bridge addressing or
// ambiguous uplink zones rather than granting cross-zone forwarding globally.
func (s Spec) PlanEgressFirewalld(uplinkZone string) ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if s.EgressNetwork == "" || s.EgressIPv4Address == "" || s.EgressBridgeIPv4CIDR == "" {
		return nil, fmt.Errorf("firewalld egress plan requires an explicit egress bridge and static LiteLLM IPv4")
	}
	if len(uplinkZone) > 64 || !firewalldZoneIdentifier.MatchString(uplinkZone) ||
		uplinkZone == "ANY" || uplinkZone == "HOST" || uplinkZone == "trusted" {
		return nil, fmt.Errorf("firewalld egress plan requires a named, non-trusted uplink zone")
	}
	prefix, err := netip.ParsePrefix(s.EgressIPv4Address)
	if err != nil {
		return nil, fmt.Errorf("invalid LiteLLM source CIDR: %w", err)
	}
	zone := "aginctus-egress"
	policy := "aginctus-egress-https"
	rejects := []string{}
	for _, subnet := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "127.0.0.0/8", "100.64.0.0/10"} {
		rejects = append(rejects, fmt.Sprintf(`rule family="ipv4" priority="-100" source address="%s/32" destination address="%s" reject`, prefix.Addr(), subnet))
	}
	out := EgressFirewalldPlan{
		Zone: zone, Interface: s.EgressNetwork, UplinkZone: uplinkZone,
		Policy: policy, Source: prefix.Addr().String() + "/32",
		RichRules: rejects,
		HTTPS:     true, Target: "CONTINUE",
		Warning: "ADVISORY ONLY: no firewall changes made; permanent firewalld object creation requires operator-controlled activation and may affect other runtime rules. Verify uplink zone, management isolation, object ownership and rollback before applying. Deny rules cover common IPv4 non-public ranges, not an exhaustive destination authorization policy.",
	}
	return json.MarshalIndent(out, "", "  ")
}
