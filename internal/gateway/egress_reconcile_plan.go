package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
)

// PlanEgressReconciliation checks the existing, owned LiteLLM instance and
// returns the two NIC settings needed for in-place reconciliation. This is
// deliberately read-only: it neither reserves an address nor mutates Incus.
// A follow-on mutation must recheck inventory and apply atomically.
func PlanEgressReconciliation(ctx context.Context, spec Spec, query func(context.Context, ...string) ([]byte, error)) (string, error) {
	if err := spec.Validate(); err != nil {
		return "", err
	}
	if spec.EgressIPv4Address == "" {
		return "", fmt.Errorf("explicit LiteLLM egress IPv4 address required for reconciliation planning")
	}
	rawNetwork, err := query(ctx, "query", "/1.0/networks/"+spec.EgressNetwork)
	if err != nil {
		return "", fmt.Errorf("inspect owned egress bridge: %w", err)
	}
	var bridge struct {
		Name    string            `json:"name"`
		Type    string            `json:"type"`
		Managed bool              `json:"managed"`
		Config  map[string]string `json:"config"`
	}
	if err := json.Unmarshal(rawNetwork, &bridge); err != nil {
		return "", fmt.Errorf("decode egress bridge: %w", err)
	}
	if bridge.Name != spec.EgressNetwork || bridge.Type != "bridge" || !bridge.Managed ||
		bridge.Config["user.aginctus.managed"] != "true" ||
		bridge.Config["user.aginctus.resource"] != "gateway-egress-network" {
		return "", fmt.Errorf("egress bridge ownership not verified")
	}
	subnet, err := netip.ParsePrefix(bridge.Config["ipv4.address"])
	if err != nil || !subnet.Addr().Is4() || !subnet.Masked().Contains(subnet.Addr()) {
		return "", fmt.Errorf("egress bridge IPv4 subnet unavailable")
	}
	address, _ := netip.ParsePrefix(spec.EgressIPv4Address) // validated by Spec
	if !subnet.Masked().Contains(address.Addr()) || address.Bits() != subnet.Bits() ||
		address.Addr() == subnet.Addr() || address.Addr() == subnet.Masked().Addr() {
		return "", fmt.Errorf("configured LiteLLM address is outside the live bridge or conflicts with its gateway")
	}
	if bridge.Config["ipv4.nat"] != "true" || bridge.Config["ipv4.routing"] != "true" {
		return "", fmt.Errorf("egress bridge NAT/routing policy not verified")
	}
	// DHCP may remain enabled only when an explicit, fully parseable pool
	// excludes the proposed static address. Incus defaults to unrestricted
	// DHCP when ipv4.dhcp is absent; such bridges remain blocked.
	dhcp := strings.ToLower(bridge.Config["ipv4.dhcp"])
	switch dhcp {
	case "false":
	case "", "true":
		if strings.TrimSpace(bridge.Config["ipv4.dhcp.ranges"]) == "" {
			return "", fmt.Errorf("egress bridge has unrestricted DHCP; explicit nonoverlapping ranges or disabled DHCP required")
		}
		pool, err := parseEgressDHCPRanges(bridge.Config["ipv4.address"], bridge.Config["ipv4.dhcp.ranges"])
		if err != nil {
			return "", fmt.Errorf("inspect egress DHCP ranges: %w", err)
		}
		for _, allocated := range pool {
			if allocated == address.Addr() {
				return "", fmt.Errorf("configured LiteLLM address overlaps the egress DHCP pool")
			}
		}
	default:
		return "", fmt.Errorf("unsupported egress DHCP setting")
	}
	// Disabled DHCP does not invalidate old leases or prevent conflicting
	// static addresses on other instances. A read-only proposal must refuse
	// any such collision rather than claim the selected address is available.
	if err := auditRequestedEgressAddress(ctx, spec.EgressNetwork, spec.Project, address.Addr(), query); err != nil {
		return "", err
	}
	rawInstance, err := query(ctx, "query", "/1.0/instances/litellm-1?project="+spec.Project+"&recursion=1")
	if err != nil {
		return "", fmt.Errorf("inspect existing LiteLLM instance: %w", err)
	}
	var instance struct {
		Name            string                       `json:"name"`
		Config          map[string]string            `json:"config"`
		Devices         map[string]map[string]string `json:"devices"`
		ExpandedDevices map[string]map[string]string `json:"expanded_devices"`
	}
	if err := json.Unmarshal(rawInstance, &instance); err != nil {
		return "", fmt.Errorf("decode LiteLLM instance: %w", err)
	}
	if instance.Name != "litellm-1" ||
		instance.Config["user.aginctus.managed"] != "true" ||
		instance.Config["user.aginctus.resource"] != "compose-instance" ||
		instance.Config["user.aginctus.deployment"] != spec.Project {
		return "", fmt.Errorf("LiteLLM instance ownership not verified")
	}
	managementName, egressName := "", ""
	for name, d := range instance.ExpandedDevices {
		if d["type"] != "nic" {
			continue
		}
		switch d["network"] {
		case spec.Network:
			if managementName != "" {
				return "", fmt.Errorf("multiple management NICs found")
			}
			managementName = name
		case spec.EgressNetwork:
			if egressName != "" {
				return "", fmt.Errorf("multiple egress NICs found")
			}
			egressName = name
		}
	}
	if managementName == "" || egressName == "" {
		return "", fmt.Errorf("LiteLLM management/egress NIC topology incomplete")
	}
	// Refuse to rewrite inherited profile devices or overwrite an existing
	// conflicting address, even in planning. This keeps the eventual
	// reconciliation narrowly scoped to owned local devices.
	if instance.Devices[managementName]["network"] != spec.Network ||
		instance.Devices[egressName]["network"] != spec.EgressNetwork {
		return "", fmt.Errorf("NICs are not both locally defined on the owned LiteLLM instance")
	}
	existing := instance.Devices[egressName]["ipv4.address"]
	if existing != "" && existing != spec.EgressIPv4Address {
		return "", fmt.Errorf("LiteLLM egress NIC already has a conflicting static address")
	}
	return fmt.Sprintf("would set %s ipv4.gateway=none; would set %s ipv4.address=%s (NOT RESERVED; no changes made)", managementName, egressName, spec.EgressIPv4Address), nil
}
