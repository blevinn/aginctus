package network

import (
	"context"
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/firewall"
	"github.com/lxc/incus/v7/shared/api"
)

// DeriveFirewallPolicy requires the caller's explicit Incus network inspection
// capability; both policy observation and CLI reporting remain read-only.
func DeriveFirewallPolicy(ctx context.Context, cfg *config.Config, getNetwork func(context.Context, string) (*api.Network, error)) (firewall.Policy, error) {
	name, err := cfg.String("incus.management.network.name")
	if err != nil {
		return firewall.Policy{}, err
	}
	address, err := cfg.String("incus.management.network.ipv4.address")
	if err != nil {
		return firewall.Policy{}, err
	}
	network, err := getNetwork(ctx, name)
	if err != nil {
		return firewall.Policy{}, err
	}
	return firewall.Derive(network, name, address, firewall.InspectLink)
}

func ReportFirewallPolicy(ctx context.Context, stdout, stderr io.Writer, cfg *config.Config, getNetwork func(context.Context, string) (*api.Network, error)) int {
	p, err := DeriveFirewallPolicy(ctx, cfg, getNetwork)
	if err != nil {
		fmt.Fprintf(stderr, "authorized firewall policy: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "authorized bridge: %s, resolver: %s, IPv4 subnet: %s; DNS UDP/TCP 53 and DHCPv4 UDP 67 only\n",
		p.Interface, p.Address, p.Subnet)
	return 0
}
