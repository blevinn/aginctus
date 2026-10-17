package firewall

import (
	"fmt"
	"net"
)

// NixosNftChain is an ordinary chain in NixOS's authoritative firewall
// table, invoked from input-allow rather than an independent base chain.
const NixosNftChain = "aginctus_dns_input"

// RenderNixosNft updates only the predeclared Aginctus-owned child chain.
func RenderNixosNft(policy *Policy) (string, error) {
	txn := "flush chain inet nixos-fw " + NixosNftChain + "\n"
	if policy == nil {
		return txn, nil
	}
	ip := net.ParseIP(policy.Address)
	_, subnet, err := net.ParseCIDR(policy.Subnet)
	if !bridgeName.MatchString(policy.Interface) || err != nil || ip == nil ||
		ip.To4() == nil || subnet == nil || !subnet.Contains(ip) {
		return "", fmt.Errorf("invalid verified NixOS nftables policy")
	}
	txn += "add rule inet nixos-fw " + NixosNftChain + " ct state invalid return\n"
	for _, proto := range []string{"udp", "tcp"} {
		txn += fmt.Sprintf("add rule inet nixos-fw %s iifname \"%s\" ip saddr %s ip daddr %s %s dport 53 accept\n",
			NixosNftChain, policy.Interface, policy.Subnet, policy.Address, proto)
	}
	txn += fmt.Sprintf("add rule inet nixos-fw %s iifname \"%s\" udp sport 68 udp dport 67 accept\n",
		NixosNftChain, policy.Interface)
	return txn, nil
}
