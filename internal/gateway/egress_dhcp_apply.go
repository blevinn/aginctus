package gateway

import (
	"context"
	"fmt"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

// EgressNetworkEditor restricts mutation to a single Incus network PUT using
// an ETag. The daemon rejects concurrent changes since GetNetwork.
type EgressNetworkEditor interface {
	GetNetwork(string) (*api.Network, string, error)
	UpdateNetwork(string, api.NetworkPut, string) error
}

func applyEgressDHCPMigration(ctx context.Context, spec Spec, ranges string, read func(context.Context, ...string) ([]byte, error), editor EgressNetworkEditor) error {
	if _, err := PlanEgressDHCPMigration(ctx, spec, ranges, read); err != nil {
		return err
	}
	network, etag, err := editor.GetNetwork(spec.EgressNetwork)
	if err != nil { return fmt.Errorf("inspect network before guarded update: %w", err) }
	if etag == "" { return fmt.Errorf("network concurrency token unavailable") }
	if network.Name != spec.EgressNetwork || network.Type != "bridge" || !network.Managed ||
		network.Config["user.aginctus.managed"] != "true" ||
		network.Config["user.aginctus.resource"] != "gateway-egress-network" ||
		network.Config["ipv4.nat"] != "true" ||
		network.Config["ipv4.routing"] != "true" ||
		network.Config["ipv6.address"] != "none" {
		return fmt.Errorf("network ownership or isolation changed before update")
	}
	// The migration preflight uses this exact CIDR. Checking it again
	// prevents a stale plan from altering a different bridge subnet.
	if spec.EgressBridgeIPv4CIDR != "" && network.Config["ipv4.address"] != spec.EgressBridgeIPv4CIDR {
		return fmt.Errorf("network CIDR changed before guarded update")
	}
	if network.Config["ipv4.dhcp"] == "false" {
		return fmt.Errorf("DHCP is already disabled")
	}
	if network.Config["ipv4.dhcp.ranges"] == ranges {
		return nil
	}
	// Recheck lease inventory immediately before the ETag-protected PUT.
	// A separate address reservation is still needed before writing NICs.
	if _, err := PlanEgressDHCPMigration(ctx, spec, ranges, read); err != nil { return err }
	put := network.Writable()
	put.Config = make(api.ConfigMap, len(network.Config)+1)
	for key, value := range network.Config { put.Config[key] = value }
	put.Config["ipv4.dhcp.ranges"] = ranges
	if err := editor.UpdateNetwork(spec.EgressNetwork, put, etag); err != nil {
		return fmt.Errorf("ETag-guarded DHCP migration failed: %w", err)
	}
	return nil
}

// ApplyEgressDHCPMigration is an explicitly invoked bridge-only mutation.
// It does not change instances, volumes, or gateway credentials.
func ApplyEgressDHCPMigration(ctx context.Context, spec Spec, ranges string, read func(context.Context, ...string) ([]byte, error)) error {
	server, err := incusclient.ConnectIncusUnixWithContext(ctx, "", &incusclient.ConnectionArgs{SkipGetServer: true})
	if err != nil { return fmt.Errorf("connect to local Incus: %w", err) }
	return applyEgressDHCPMigration(ctx, spec, ranges, read, server)
}
