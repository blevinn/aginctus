package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
)

// PlanEgressDHCPMigration verifies that restricting the existing owned bridge
// will not exclude any active IPv4 lease. This is read-only, not a reservation
// or authorization to change Incus network settings.
func PlanEgressDHCPMigration(ctx context.Context, spec Spec, ranges string, query func(context.Context, ...string) ([]byte, error)) (string, error) {
	if err := spec.Validate(); err != nil {
		return "", err
	}
	if spec.EgressNetwork == "" || spec.EgressIPv4Address == "" {
		return "", fmt.Errorf("egress network and static LiteLLM IPv4 CIDR must be configured")
	}
	if strings.TrimSpace(ranges) == "" {
		return "", fmt.Errorf("proposed DHCP range must not be empty")
	}
	raw, err := query(ctx, "query", "/1.0/networks/"+spec.EgressNetwork)
	if err != nil { return "", fmt.Errorf("inspect egress bridge: %w", err) }
	var network struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Managed bool `json:"managed"`
		Config map[string]string `json:"config"`
	}
	if err := json.Unmarshal(raw, &network); err != nil { return "", fmt.Errorf("decode egress bridge: %w", err) }
	if network.Name != spec.EgressNetwork || network.Type != "bridge" || !network.Managed ||
		network.Config["user.aginctus.managed"] != "true" ||
		network.Config["user.aginctus.resource"] != "gateway-egress-network" ||
		network.Config["ipv4.nat"] != "true" || network.Config["ipv4.routing"] != "true" ||
		network.Config["ipv6.address"] != "none" {
		return "", fmt.Errorf("egress bridge ownership or isolation cannot be verified")
	}
	bridge, err := netip.ParsePrefix(network.Config["ipv4.address"])
	if err != nil || !bridge.Addr().Is4() || bridge.Bits() < 16 || bridge.Bits() > 30 {
		return "", fmt.Errorf("invalid live egress bridge subnet")
	}
	host, _ := netip.ParsePrefix(spec.EgressIPv4Address) // Spec.Validate checked CIDR syntax
	if host.Bits() != bridge.Bits() || !bridge.Masked().Contains(host.Addr()) ||
		host.Addr() == bridge.Addr() || host.Addr() == bridge.Masked().Addr() {
		return "", fmt.Errorf("static LiteLLM address is incompatible with the live bridge")
	}
	pool, err := parseEgressDHCPRanges(network.Config["ipv4.address"], ranges)
	if err != nil { return "", fmt.Errorf("proposed DHCP pool: %w", err) }
	addresses := make(map[netip.Addr]bool, len(pool))
	for _, ip := range pool { addresses[ip] = true }
	if addresses[host.Addr()] {
		return "", fmt.Errorf("proposed DHCP pool overlaps configured static LiteLLM address")
	}
	if addresses[bridge.Addr()] || addresses[bridge.Masked().Addr()] {
		return "", fmt.Errorf("proposed DHCP pool contains the bridge gateway or network address")
	}
	// Preserve all existing leases, regardless of whether DHCP previously
	// allocated them or they were pinned to a fixed address.
	leasesRaw, err := query(ctx, "query", "/1.0/networks/"+spec.EgressNetwork+"/leases")
	if err != nil { return "", fmt.Errorf("inspect existing egress leases: %w", err) }
	var leases []struct { Address string `json:"address"` }
	if err := json.Unmarshal(leasesRaw, &leases); err != nil || leases == nil {
		return "", fmt.Errorf("incomplete egress lease inventory")
	}
	for _, lease := range leases {
		ip, err := netip.ParseAddr(lease.Address)
		if err != nil { return "", fmt.Errorf("invalid egress lease inventory") }
		if ip.Is4() && !addresses[ip] {
			return "", fmt.Errorf("proposed DHCP pool excludes an existing IPv4 lease")
		}
	}
	return fmt.Sprintf("egress bridge %s: proposed ipv4.dhcp.ranges=%s preserves %d lease(s) and excludes LiteLLM %s (READ-ONLY; NOT RESERVED)", spec.EgressNetwork, ranges, len(leases), spec.EgressIPv4Address), nil
}
