package cli

import (
	"context"
	"fmt"
	"io"
	"net"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/firewall"
	"github.com/blevinn/aginctus/internal/gateway"
)

type egressStateFile = firewall.EgressStateFile

var lockEgress = firewall.LockEgress

var inspectEgressLink = firewall.InspectLink
var effectiveEgressZone = firewall.Busctl{}.EffectiveZone

func runNetworkFirewallEgress(ctx context.Context, stdout, stderr io.Writer, cfg *config.Config, revoke bool) int {
	if firewallEffectiveUID() != 0 {
		fmt.Fprintln(stderr, "egress firewall reconciliation requires root; no authorization prompt attempted")
		return 1
	}
	unlock, err := lockEgress("/run/aginctus-firewall")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer unlock()
	bridge, err := cfg.String("gateway.egress.network.name")
	if err != nil || bridge == "" {
		fmt.Fprintln(stderr, "configured egress bridge required")
		return 1
	}
	discover := func(ctx context.Context) (firewall.EgressGrant, error) {
		if revoke {
			return firewall.EgressGrant{}, nil
		}
		return discoverEgressGrant(ctx, cfg)
	}
	if err := firewall.ReconcileEgress(ctx, firewall.Busctl{}, egressStateFile("/run/aginctus-firewall/state.json"), bridge, discover); err != nil {
		fmt.Fprintf(stderr, "egress firewall: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "egress runtime permissions reconciled; permanent default-deny boundary unchanged")
	return 0
}

func discoverEgressGrant(ctx context.Context, cfg *config.Config) (firewall.EgressGrant, error) {
	var empty firewall.EgressGrant
	uplink, err := cfg.String("firewall.egress.uplinkZone")
	if err != nil || uplink == "" {
		return empty, fmt.Errorf("firewall.egress.uplinkZone must name a nontrusted uplink zone")
	}
	allowTailscaleRouting, err := cfg.Bool("firewall.egress.allowTailscaleRouting")
	if err != nil {
		return empty, err
	}
	spec, err := gateway.FromConfig(cfg)
	if err != nil {
		return empty, err
	}
	// Reuse all-project collision, DHCP reservation and exact NIC ownership checks.
	if _, err = gateway.PlanEgressReconciliation(ctx, spec, gatewayEgressQuery); err != nil {
		return empty, err
	}
	if err = gateway.AuditEgressFirewallSource(ctx, spec, gatewayEgressQuery); err != nil {
		return empty, err
	}
	network, err := getFirewallNetwork(ctx, spec.EgressNetwork)
	if err != nil {
		return empty, err
	}
	if network.Config["ipv6.address"] != "none" || network.Config["ipv4.address"] != spec.EgressBridgeIPv4CIDR {
		return empty, fmt.Errorf("egress bridge addressing drift")
	}
	ip, _, err := net.ParseCIDR(spec.EgressBridgeIPv4CIDR)
	if err != nil {
		return empty, err
	}
	if err = inspectEgressLink(spec.EgressNetwork, ip); err != nil {
		return empty, err
	}
	// Never use a guessed physical interface. Routing decides the uplink;
	// its zone must match the explicit configuration before opening HTTPS.
	rules, err := firewallCommand(ctx, "", "ip", "-j", "-4", "rule", "show")
	if err != nil {
		return empty, err
	}
	raw, err := firewallCommand(ctx, "", "ip", "-j", "-4", "route", "show", "table", "all")
	if err != nil {
		return empty, err
	}
	iface, blocked, err := firewall.EgressRouting(rules, raw, allowTailscaleRouting)
	if err != nil {
		return empty, err
	}
	if iface == spec.EgressNetwork || iface == spec.Network {
		return empty, fmt.Errorf("unsafe default route")
	}
	zone, err := effectiveEgressZone(ctx, iface)
	if err != nil || zone != uplink {
		return empty, fmt.Errorf("default-route interface does not belong to configured uplink zone")
	}
	return firewall.EgressRules(spec.EgressIPv4Address, spec.EgressBridgeIPv4CIDR, uplink, blocked)
}
