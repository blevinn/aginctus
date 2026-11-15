package cli

import (
	"context"
	"io"

	networkops "github.com/blevinn/aginctus/internal/cli/operations/network"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/host/net/firewall"
)

type egressStateFile = firewall.EgressStateFile

var lockEgress = firewall.LockEgress
var inspectEgressLink = firewall.InspectLink
var effectiveEgressZone = firewall.Busctl{}.EffectiveZone

func networkEgressActions() networkops.EgressActions {
	return networkops.EgressActions{
		EffectiveUID: firewallEffectiveUID,
		Lock: lockEgress,
		InspectLink: inspectEgressLink,
		EffectiveZone: effectiveEgressZone,
		ReadIncus: gatewayEgressQuery,
		Network: getFirewallNetwork,
		Command: firewallCommand,
	}
}

func runNetworkFirewallEgress(ctx context.Context, stdout, stderr io.Writer, cfg *config.Config, revoke bool) int {
	return networkops.ReconcileEgress(ctx, stdout, stderr, cfg, revoke, networkEgressActions())
}

func discoverEgressGrant(ctx context.Context, cfg *config.Config) (firewall.EgressGrant, error) {
	return networkops.DiscoverEgressGrant(ctx, cfg, networkEgressActions())
}
