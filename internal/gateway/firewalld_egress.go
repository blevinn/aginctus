package gateway

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"

	"github.com/blevinn/aginctus/internal/firewall"
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
	grant, err := firewall.EgressRules(s.EgressIPv4Address, s.EgressBridgeIPv4CIDR, uplinkZone, nil)
	if err != nil {
		return nil, err
	}
	out := EgressFirewalldPlan{
		Zone: zone, Interface: s.EgressNetwork, UplinkZone: uplinkZone,
		Policy: policy, Source: prefix.Addr().String() + "/32",
		RichRules: grant.HTTPS[:len(grant.HTTPS)-1],
		HTTPS:     true, Target: "CONTINUE",
		Warning: "ADVISORY ONLY: no firewall changes made. NixOS owns the default-deny bootstrap. Runtime reconciliation additionally excludes live host routes and verifies bridge, NIC, address ownership and the uplink. This plan alone is not activation approval or a complete confinement policy.",
	}
	return json.MarshalIndent(out, "", "  ")
}
