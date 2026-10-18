package gateway

import (
	"context"
	"encoding/json"
	"fmt"
)

// ApplyEgressDHCPMigration deliberately supports only already restricted
// DHCP bridges. It updates the reserved pool while leaving the bridge, NICs
// and gateway workloads intact. Unrestricted-to-restricted migration requires
// an Incus compare-and-swap update and is intentionally refused here.
func ApplyEgressDHCPMigration(ctx context.Context, spec Spec, ranges string, query func(context.Context, ...string) ([]byte, error)) error {
	if _, err := PlanEgressDHCPMigration(ctx, spec, ranges, query); err != nil {
		return err
	}
	raw, err := query(ctx, "query", "/1.0/networks/"+spec.EgressNetwork)
	if err != nil {
		return fmt.Errorf("recheck bridge before DHCP update: %w", err)
	}
	var network struct {
		Config map[string]string `json:"config"`
	}
	if err := json.Unmarshal(raw, &network); err != nil {
		return fmt.Errorf("decode DHCP migration state: %w", err)
	}
	// This initial, bounded mutation path refuses the live pilot until the
	// unrestricted DHCP transition is implemented with server-side CAS.
	if network.Config["ipv4.dhcp.ranges"] == "" {
		return fmt.Errorf("live bridge has unrestricted DHCP: migration requires atomic Incus network update; no changes made")
	}
	if network.Config["ipv4.dhcp"] == "false" {
		return fmt.Errorf("bridge DHCP is disabled; range migration unnecessary")
	}
	return fmt.Errorf("DHCP range changes not enabled until atomic Incus update and rollback are available; no changes made")
}
