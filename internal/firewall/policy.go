// Package firewall derives minimal guest-to-host permissions from verified
// Incus bridge ownership, never from a naming prefix or static subnet.
package firewall

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/lxc/incus/v7/shared/api"
)

type Policy struct {
	Interface string
	Address   string
	Subnet    string
}

// LinkInspection is injectable to let policy tests run without a real bridge.
type LinkInspection func(name string, expected net.IP) error

func InspectLink(name string, expected net.IP) error {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return fmt.Errorf("bridge interface not found: %w", err)
	}
	if _, err := os.Stat(filepath.Join("/sys/class/net", name, "bridge")); err != nil {
		return fmt.Errorf("Incus interface is not a Linux bridge: %w", err)
	}
	addresses, err := iface.Addrs()
	if err != nil {
		return fmt.Errorf("read bridge addresses: %w", err)
	}
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err == nil && ip.Equal(expected) {
			return nil
		}
	}
	return fmt.Errorf("Incus bridge address does not match live interface")
}

// Derive checks the selected, expected managed network, not all resources
// claiming user.aginctus.managed=true. The configured name is an identity
// selector; it never independently grants permission.
func Derive(network *api.Network, configuredName, configuredAddress string, inspect LinkInspection) (Policy, error) {
	if !bridgeName.MatchString(configuredName) {
		return Policy{}, fmt.Errorf("unsafe selected bridge interface name")
	}
	if network == nil || configuredName == "" || network.Name != configuredName ||
		!network.Managed || network.Type != "bridge" ||
		network.Config["user.aginctus.managed"] != "true" ||
		network.Config["user.aginctus.resource"] != "management-network" {
		return Policy{}, fmt.Errorf("Incus network identity or ownership mismatch")
	}
	if network.Status != "" && network.Status != "Created" {
		return Policy{}, fmt.Errorf("Incus bridge is not in Created state")
	}
	if network.Config["ipv4.nat"] != "false" || network.Config["ipv4.routing"] != "false" ||
		network.Config["ipv6.address"] != "none" {
		return Policy{}, fmt.Errorf("management network isolation differs from supported policy")
	}
	cidr := network.Config["ipv4.address"]
	ip, subnet, err := net.ParseCIDR(cidr)
	if err != nil || ip.To4() == nil || ip.IsUnspecified() || ip.IsMulticast() {
		return Policy{}, fmt.Errorf("management network has no valid IPv4 bridge address")
	}
	if configuredAddress != "auto" && configuredAddress != cidr {
		return Policy{}, fmt.Errorf("management network IPv4 address differs from configuration")
	}
	if inspect == nil {
		return Policy{}, fmt.Errorf("bridge interface verification unavailable")
	}
	if err := inspect(network.Name, ip); err != nil {
		return Policy{}, fmt.Errorf("bridge interface verification failed: %w", err)
	}
	return Policy{Interface: network.Name, Address: ip.String(), Subnet: subnet.String()}, nil
}
