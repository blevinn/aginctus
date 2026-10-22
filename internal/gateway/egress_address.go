package gateway

import (
	"fmt"
	"net/netip"
)

// selectEgressIPv4 picks a host CIDR on an existing, owned egress bridge.
// Callers must supply ALL unavailable addresses (including DHCP allocations
// and reservations) and prove DHCP cannot subsequently allocate the candidate.
// In particular, this helper must not be used against an unrestricted DHCP pool.
func selectEgressIPv4(bridgeCIDR string, unavailable []netip.Addr) (string, error) {
	bridge, err := netip.ParsePrefix(bridgeCIDR)
	if err != nil || !bridge.IsValid() || !bridge.Addr().Is4() ||
		bridge.Bits() < 16 || bridge.Bits() > 30 {
		return "", fmt.Errorf("egress bridge must have a supported IPv4 CIDR")
	}
	network := bridge.Masked()
	gateway := bridge.Addr()
	if !network.Contains(gateway) || gateway == network.Addr() {
		return "", fmt.Errorf("egress bridge gateway must be a usable IPv4 host address")
	}
	blocked := make(map[netip.Addr]bool, len(unavailable)+2)
	blocked[gateway] = true
	blocked[network.Addr()] = true
	for _, addr := range unavailable {
		if !addr.Is4() || !network.Contains(addr) {
			return "", fmt.Errorf("unavailable address is outside the egress bridge subnet")
		}
		blocked[addr] = true
	}
	// Prefer high addresses to make the result reproducible. The final address
	// of a subnet is broadcast, and must never be selected.
	last := network.Addr()
	for addr := last.Next(); network.Contains(addr); addr = addr.Next() {
		last = addr
	}
	for candidate := last.Prev(); network.Contains(candidate); candidate = candidate.Prev() {
		if !blocked[candidate] && candidate != network.Addr() {
			return netip.PrefixFrom(candidate, network.Bits()).String(), nil
		}
		if candidate == network.Addr() {
			break
		}
	}
	return "", fmt.Errorf("no free IPv4 address available for LiteLLM egress")
}
