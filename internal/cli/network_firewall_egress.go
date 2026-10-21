package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"

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
	if err = verifyEgressRoutingRules(rules); err != nil {
		return empty, err
	}
	raw, err := firewallCommand(ctx, "", "ip", "-j", "-4", "route", "show", "table", "all")
	if err != nil {
		return empty, err
	}
	var routes []struct {
		Dst      string            `json:"dst"`
		Dev      string            `json:"dev"`
		Type     string            `json:"type"`
		Nexthops []json.RawMessage `json:"nexthops"`
		NHID     json.RawMessage   `json:"nhid"`
	}
	if err = json.Unmarshal([]byte(raw), &routes); err != nil {
		return empty, err
	}
	var blocked []netip.Prefix
	defaults := 0
	for _, route := range routes {
		if len(route.Nexthops) != 0 || len(route.NHID) != 0 {
			return empty, fmt.Errorf("multipath routes are unsupported")
		}
		if route.Dst == "default" {
			if (route.Type != "" && route.Type != "unicast") || route.Dev == "" || route.Dev == spec.EgressNetwork || route.Dev == spec.Network {
				return empty, fmt.Errorf("unsafe default route")
			}
			zone, err := effectiveEgressZone(ctx, route.Dev)
			if err != nil || zone != uplink {
				return empty, fmt.Errorf("default-route interface does not belong to configured uplink zone")
			}
			defaults++
			continue
		}
		if route.Dst == "" {
			return empty, fmt.Errorf("opaque route inventory")
		}
		prefix, err := netip.ParsePrefix(route.Dst)
		if err != nil {
			if address, e := netip.ParseAddr(route.Dst); e == nil {
				prefix = netip.PrefixFrom(address, 32)
			} else {
				return empty, err
			}
		}
		blocked = append(blocked, prefix)
	}
	if defaults != 1 {
		return empty, fmt.Errorf("exactly one IPv4 default route required")
	}
	return firewall.EgressRules(spec.EgressIPv4Address, spec.EgressBridgeIPv4CIDR, uplink, blocked)
}

// Custom routing rules can select paths not represented by the default route.
// Refuse policy routing rather than infer a safe path from partial inventory.
func verifyEgressRoutingRules(raw string) error {
	var rules []map[string]any
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return err
	}
	if len(rules) != 3 {
		return fmt.Errorf("custom IPv4 policy routing is unsupported")
	}
	for i, table := range []string{"local", "main", "default"} {
		priority := []float64{0, 32766, 32767}[i]
		if rules[i]["src"] != "all" || rules[i]["table"] != table || rules[i]["priority"] != priority {
			return fmt.Errorf("custom IPv4 policy routing is unsupported")
		}
		for key := range rules[i] {
			if key != "src" && key != "table" && key != "priority" && key != "protocol" {
				return fmt.Errorf("unsupported IPv4 routing rule selector: %s", key)
			}
		}
	}
	return nil
}
