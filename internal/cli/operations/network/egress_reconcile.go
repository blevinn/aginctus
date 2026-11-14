package network

import (
	"context"
	"fmt"
	"io"
	"net"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/firewall"
	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/lxc/incus/v7/shared/api"
)

// EgressActions explicitly supplies identity, scope, read-only route/network
// observations, and the authorized host command/lease capabilities.
type EgressActions struct {
	EffectiveUID func() int
	Lock func(string) (func(), error)
	InspectLink func(string, net.IP) error
	EffectiveZone func(context.Context, string) (string, error)
	ReadIncus func(context.Context, ...string) ([]byte, error)
	Network func(context.Context, string) (*api.Network, error)
	Command func(context.Context, string, string, ...string) (string, error)
}

func ReconcileEgress(ctx context.Context, stdout, stderr io.Writer, cfg *config.Config, revoke bool, actions EgressActions) int {
	if actions.EffectiveUID() != 0 {
		fmt.Fprintln(stderr, "egress firewall reconciliation requires root; no authorization prompt attempted")
		return 1
	}
	unlock, err := actions.Lock("/run/aginctus-firewall")
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
		return DiscoverEgressGrant(ctx, cfg, actions)
	}
	if err := firewall.ReconcileEgress(ctx, firewall.Busctl{}, firewall.EgressStateFile("/run/aginctus-firewall/state.json"), bridge, discover); err != nil {
		fmt.Fprintf(stderr, "egress firewall: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "egress runtime permissions reconciled; permanent default-deny boundary unchanged")
	return 0
}

func DiscoverEgressGrant(ctx context.Context, cfg *config.Config, actions EgressActions) (firewall.EgressGrant, error) {
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
	if _, err = gateway.PlanEgressReconciliation(ctx, spec, actions.ReadIncus); err != nil {
		return empty, err
	}
	if err = gateway.AuditEgressFirewallSource(ctx, spec, actions.ReadIncus); err != nil {
		return empty, err
	}
	network, err := actions.Network(ctx, spec.EgressNetwork)
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
	if err = actions.InspectLink(spec.EgressNetwork, ip); err != nil {
		return empty, err
	}
	// Never use a guessed physical interface. Routing decides the uplink;
	// its zone must match the explicit configuration before opening HTTPS.
	rules, err := actions.Command(ctx, "", "ip", "-j", "-4", "rule", "show")
	if err != nil {
		return empty, err
	}
	raw, err := actions.Command(ctx, "", "ip", "-j", "-4", "route", "show", "table", "all")
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
	zone, err := actions.EffectiveZone(ctx, iface)
	if err != nil || zone != uplink {
		return empty, fmt.Errorf("default-route interface does not belong to configured uplink zone")
	}
	return firewall.EgressRules(spec.EgressIPv4Address, spec.EgressBridgeIPv4CIDR, uplink, blocked)
}
