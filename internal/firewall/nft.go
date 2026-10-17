package firewall

import (
	"fmt"
	"net"
)

// FirewalldChain is an ordinary chain inside the *live firewalld table*,
// entered by a jump in firewalld's actual filter_INPUT chain before reject.
// An independent input base chain would not override firewalld's decision.
const FirewalldChain = "aginctus_dns_input"
const FirewalldHookComment = "aginctus:guest-host-dns-dhcp"

// RenderFirewalld produces an atomic nft transaction, never a shell script.
// It changes only Aginctus's chain and its narrowly scoped INPUT jump.
func RenderFirewalld(policy *Policy, chainExists, hookExists bool) (string, error) {
	if hookExists && !chainExists {
		return "", fmt.Errorf("firewalld hook exists without Aginctus chain")
	}
	if policy != nil {
		ip := net.ParseIP(policy.Address)
		_, cidr, err := net.ParseCIDR(policy.Subnet)
		if !bridgeName.MatchString(policy.Interface) || ip == nil || ip.To4() == nil ||
			cidr == nil || err != nil || !cidr.Contains(ip) {
			return "", fmt.Errorf("invalid firewalld bridge policy")
		}
	}
	transaction := ""
	if !chainExists {
		transaction += "add chain inet firewalld " + FirewalldChain + "\n"
	} else {
		transaction += "flush chain inet firewalld " + FirewalldChain + "\n"
	}
	if !hookExists {
		transaction += "insert rule inet firewalld filter_INPUT jump " + FirewalldChain +
			" comment \"" + FirewalldHookComment + "\"\n"
	}
	if policy != nil {
		for _, proto := range []string{"udp", "tcp"} {
			transaction += fmt.Sprintf(
				"add rule inet firewalld %s iifname \"%s\" ip saddr %s ip daddr %s %s dport 53 accept\n",
				FirewalldChain, policy.Interface, policy.Subnet, policy.Address, proto)
		}
		transaction += fmt.Sprintf(
			"add rule inet firewalld %s iifname \"%s\" udp sport 68 udp dport 67 accept\n",
			FirewalldChain, policy.Interface)
	}
	return transaction, nil
}
