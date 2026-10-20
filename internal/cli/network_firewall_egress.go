package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/firewall"
	"github.com/blevinn/aginctus/internal/gateway"
	"golang.org/x/sys/unix"
)

type egressStateFile string

var inspectEgressLink = firewall.InspectLink
var effectiveEgressZone = firewall.Busctl{}.EffectiveZone

func (path egressStateFile) Load() (firewall.EgressState, error) {
	var state firewall.EgressState
	raw, err := os.ReadFile(string(path))
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(raw, &state)
	return state, err
}

func (path egressStateFile) Save(state firewall.EgressState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(string(path)), ".egress-state-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), string(path))
}

func lockEgress(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	fd, err := unix.Open(filepath.Join(dir, "lock"), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("another Aginctus firewall operation holds the lifecycle lock: %w", err)
	}
	return func() { unix.Close(fd) }, nil
}

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
	raw, err := firewallCommand(ctx, "", "ip", "-j", "-4", "route", "show", "table", "all")
	if err != nil {
		return empty, err
	}
	var routes []struct {
		Dst  string `json:"dst"`
		Dev  string `json:"dev"`
		Type string `json:"type"`
	}
	if err = json.Unmarshal([]byte(raw), &routes); err != nil {
		return empty, err
	}
	var blocked []netip.Prefix
	defaults := 0
	for _, route := range routes {
		if route.Dst == "default" {
			if route.Dev == "" || route.Dev == spec.EgressNetwork || route.Dev == spec.Network {
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
