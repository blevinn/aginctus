package firewall

import (
	"fmt"
	"net"
	"regexp"
)

// Firewalld limits zone file names to 17 characters. Limit the configurable
// name to an Aginctus namespace to avoid accidentally managing an unrelated
// host zone such as "trusted" or "public".
var firewalldZoneName = regexp.MustCompile(`^aginctus-[a-z0-9][a-z0-9-]*package firewall

import (
	"fmt"
	"net"
	"regexp"
)

// FirewalldZone is installed declaratively by the NixOS module. Its DROP
// target, empty permanent permissions and absence of permanent interfaces
// are security invariants. Runtime exceptions belong exclusively to Aginctus.
const FirewalldZone = "aginctus-dns"

)

func ValidateFirewalldZone(name string) error {
	if len(name) > 17 || !firewalldZoneName.MatchString(name) {
		return fmt.Errorf("invalid Aginctus firewalld zone name: require aginctus- prefix, lowercase letters/digits/hyphens, at most 17 characters")
	}
	return nil
}

// FirewalldRules renders firewalld rich rules, never nftables syntax. The
// zone's interface binding supplies the ingress-interface restriction.
func FirewalldRules(policy *Policy) ([]string, error) {
	if policy == nil {
		return nil, nil
	}
	ip := net.ParseIP(policy.Address)
	_, subnet, err := net.ParseCIDR(policy.Subnet)
	if !bridgeName.MatchString(policy.Interface) || ip == nil || ip.To4() == nil ||
		err != nil || subnet == nil || !subnet.Contains(ip) {
		return nil, fmt.Errorf("invalid firewalld bridge policy")
	}
	rules := make([]string, 0, 3)
	for _, proto := range []string{"udp", "tcp"} {
		rules = append(rules, fmt.Sprintf(
			`rule family="ipv4" source address="%s" destination address="%s" port port="53" protocol="%s" accept`,
			policy.Subnet, policy.Address, proto))
	}
	// DHCP DISCOVER has source 0.0.0.0 and broadcast destination. The
	// firewalld rich-language API cannot combine source-port and destination-
	// port matches, so constrain UDP 67 by ingress interface (zone) instead.
	rules = append(rules, `rule family="ipv4" port port="67" protocol="udp" accept`)
	return rules, nil
}
