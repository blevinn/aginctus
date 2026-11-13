package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/firewall"
	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

var getFirewallNetwork = func(ctx context.Context, name string) (*api.Network, error) {
	server, err := incusclient.ConnectIncusUnixWithContext(ctx, "", &incusclient.ConnectionArgs{SkipGetServer: true})
	if err != nil {
		return nil, fmt.Errorf("connect to Incus: %w", err)
	}
	network, _, err := server.GetNetwork(name)
	if err != nil {
		return nil, fmt.Errorf("get configured Incus network: %w", err)
	}
	return network, nil
}

func deriveFirewallPolicy(ctx context.Context, cfg *config.Config) (firewall.Policy, error) {
	name, err := cfg.String("incus.management.network.name")
	if err != nil {
		return firewall.Policy{}, err
	}
	address, err := cfg.String("incus.management.network.ipv4.address")
	if err != nil {
		return firewall.Policy{}, err
	}
	network, err := getFirewallNetwork(ctx, name)
	if err != nil {
		return firewall.Policy{}, err
	}
	return firewall.Derive(network, name, address, firewall.InspectLink)
}

func runNetworkFirewallPolicy(ctx context.Context, stdout, stderr io.Writer, cfg *config.Config) int {
	p, err := deriveFirewallPolicy(ctx, cfg)
	if err != nil {
		fmt.Fprintf(stderr, "authorized firewall policy: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "authorized bridge: %s, resolver: %s, IPv4 subnet: %s; DNS UDP/TCP 53 and DHCPv4 UDP 67 only\n",
		p.Interface, p.Address, p.Subnet)
	return 0
}
