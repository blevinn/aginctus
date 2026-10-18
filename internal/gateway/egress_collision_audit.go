package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
)

// auditRequestedEgressAddress refuses addresses with existing DHCP leases or
// static assignments. An inventory snapshot is not an atomic reservation.
func auditRequestedEgressAddress(ctx context.Context, network string, requested netip.Addr, query func(context.Context, ...string) ([]byte, error)) error {
	raw, err := query(ctx, "query", "/1.0/networks/"+network+"/leases")
	if err != nil {
		return fmt.Errorf("inspect egress address leases: %w", err)
	}
	if string(raw) == "null" {
		return fmt.Errorf("egress lease inventory unavailable")
	}
	var leases []struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal(raw, &leases); err != nil || leases == nil {
		return fmt.Errorf("incomplete egress lease inventory")
	}
	for _, lease := range leases {
		assigned, err := netip.ParseAddr(lease.Address)
		if err != nil {
			return fmt.Errorf("invalid egress lease inventory")
		}
		if assigned == requested {
			return fmt.Errorf("requested egress address has an existing lease")
		}
	}
	// The existing inventory intentionally fails closed on NICs without
	// explicit addresses, including on projects other than this gateway.
	assigned, err := auditEgressInstances(ctx, network, query)
	if err != nil {
		return fmt.Errorf("cannot exclude cross-project egress address collision: %w", err)
	}
	for _, addr := range assigned {
		if addr == requested {
			return fmt.Errorf("requested egress address is already assigned")
		}
	}
	return nil
}
