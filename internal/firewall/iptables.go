package firewall

import (
	"fmt"
	"net"
	"regexp"
)

// Chain is installed in NixOS's effective INPUT decision path, not an
// independent nftables base chain.
const Chain = "AGINCTUS_INPUT"

var bridgeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,14}$`)

// Render atomically replaces only the Aginctus-owned chain. A nil policy
// clears obsolete permissions when a network has been deleted or disowned.
func Render(policy *Policy) (string, error) {
	transaction := "*filter\n:" + Chain + " - [0:0]\n" + "-F " + Chain + "\n"
	if policy == nil {
		return transaction + "COMMIT\n", nil
	}
	ip := net.ParseIP(policy.Address)
	_, subnet, err := net.ParseCIDR(policy.Subnet)
	if !bridgeName.MatchString(policy.Interface) || ip == nil || ip.To4() == nil ||
		subnet == nil || !subnet.Contains(ip) || err != nil {
		return "", fmt.Errorf("invalid owned-bridge firewall policy")
	}
	// Our jump may be ahead of the host conntrack-invalid filter. Do not
	// accept INVALID packets even when they match DNS/DHCP ports.
	transaction += "-A " + Chain + " -m conntrack --ctstate INVALID -j RETURN\n"
	for _, protocol := range []string{"udp", "tcp"} {
		transaction += fmt.Sprintf("-A %s -i %s -s %s -d %s -p %s --dport 53 -j ACCEPT\n",
			Chain, policy.Interface, policy.Subnet, policy.Address, protocol)
	}
	// DHCPv4 broadcast (255.255.255.255) cannot be constrained to the
	// bridge unicast IP. Constrain interface, source and destination ports.
	transaction += fmt.Sprintf("-A %s -i %s -p udp --sport 68 --dport 67 -j ACCEPT\n",
		Chain, policy.Interface)
	return transaction + "COMMIT\n", nil
}
