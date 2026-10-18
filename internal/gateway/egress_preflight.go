package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os/exec"
	"strings"
)

// EgressAddressPreflight is read-only. A candidate is advisory, never a reservation.
func EgressAddressPreflight(ctx context.Context, name string) (string, error) {
	return egressAddressPreflight(ctx, name, func(ctx context.Context, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "incus", args...)
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("inspect egress network: %w", err)
		}
		return out, nil
	})
}

func egressAddressPreflight(ctx context.Context, name string, run func(context.Context, ...string) ([]byte, error)) (string, error) {
	if !gatewayIDPattern.MatchString(name) {
		return "", fmt.Errorf("invalid egress network name")
	}
	raw, err := run(ctx, "query", "/1.0/networks/"+name)
	if err != nil {
		return "", err
	}
	var network struct {
		Name    string            `json:"name"`
		Type    string            `json:"type"`
		Managed bool              `json:"managed"`
		Config  map[string]string `json:"config"`
	}
	if err := json.Unmarshal(raw, &network); err != nil {
		return "", fmt.Errorf("decode network inventory: %w", err)
	}
	if network.Name != name || network.Type != "bridge" || !network.Managed ||
		network.Config["user.aginctus.managed"] != "true" ||
		network.Config["user.aginctus.resource"] != "gateway-egress-network" {
		return "", fmt.Errorf("egress network ownership or bridge type is not verified")
	}
	if network.Config["ipv4.nat"] != "true" || network.Config["ipv4.routing"] != "true" ||
		network.Config["ipv6.address"] != "none" {
		return "", fmt.Errorf("egress network isolation configuration is not verified")
	}
	// Incus bridges normally enable DHCP. Do not recommend an address from a live DHCP pool.
	if strings.ToLower(network.Config["ipv4.dhcp"]) != "false" {
		return "", fmt.Errorf("egress address allocation blocked: DHCP not explicitly disabled; no reservation is held")
	}
	if strings.TrimSpace(network.Config["ipv4.address"]) == "" {
		return "", fmt.Errorf("egress bridge has no IPv4 CIDR")
	}
	leases, err := run(ctx, "query", "/1.0/networks/"+name+"/leases")
	if err != nil {
		return "", err
	}
	var entries []struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal(leases, &entries); err != nil {
		return "", fmt.Errorf("decode leases: %w", err)
	}
	unavailable := []netip.Addr{}
	for _, entry := range entries {
		if entry.Address == "" {
			return "", fmt.Errorf("incomplete lease inventory")
		}
		addr, err := netip.ParseAddr(entry.Address)
		if err != nil {
			return "", fmt.Errorf("invalid lease inventory entry")
		}
		if addr.Is4() {
			unavailable = append(unavailable, addr)
		}
	}
	candidate, err := selectEgressIPv4(network.Config["ipv4.address"], unavailable)
	if err != nil {
		return "", err
	}
	// Address selection does not reserve it or check static allocations.
	return "", fmt.Errorf("candidate %s is unreserved: static assignments and allocation races not yet checked", candidate)
}
