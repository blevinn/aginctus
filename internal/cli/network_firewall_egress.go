package cli

import (
	"context"
	"io"

	networkops "github.com/blevinn/aginctus/internal/cli/operations/network"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/host/net/firewall"
)

type egressStateFile = firewall.EgressStateFile

func networkEgressActions() networkops.EgressActions {
	return networkops.EgressActions{
		EffectiveUID: firewallEffectiveUID,
		Lock: firewall.LockEgress,
		InspectLink: firewall.InspectLink,
		EffectiveZone: firewall.Busctl{}.EffectiveZone,
		ReadIncus: gatewayops.QueryIncusEgress,
		Network: getFirewallNetwork,
		Command: firewallCommand,
	}
}

func runNetworkFirewallEgress(ctx context.Context, stdout, stderr io.Writer, cfg *config.Config, revoke bool) int {
	return runNetworkFirewallEgressWithActions(ctx, stdout, stderr, cfg, revoke, networkEgressActions())
}

// Egress authorization and route/Incus observations are bound per invocation.
// The host domain independently checks effective-root before touching the lock.
func runNetworkFirewallEgressWithActions(ctx context.Context, stdout, stderr io.Writer, cfg *config.Config, revoke bool, actions networkops.EgressActions) int {
	return networkops.ReconcileEgress(ctx, stdout, stderr, cfg, revoke, actions)
}

func discoverEgressGrant(ctx context.Context, cfg *config.Config) (firewall.EgressGrant, error) {
	return discoverEgressGrantWithActions(ctx, cfg, networkEgressActions())
}

func discoverEgressGrantWithActions(ctx context.Context, cfg *config.Config, actions networkops.EgressActions) (firewall.EgressGrant, error) {
	return networkops.DiscoverEgressGrant(ctx, cfg, actions)
}
