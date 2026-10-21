package firewall

import (
	"net/netip"
	"strings"
	"testing"
)

func TestEgressContractRejectsSecurityDrift(t *testing.T) {
	for _, field := range []string{"helpers", "sources", "source_ports", "forward_ports", "future_bypass", "icmp_block_inversion", "target", "description"} {
		t.Run(field, func(t *testing.T) {
			want := EgressObjects("egress-test")[EgressHTTPS]
			got := EgressObjects("egress-test")[EgressHTTPS]
			got[field] = "unexpected"
			if VerifySettings(got, want) == nil {
				t.Fatal("accepted security drift")
			}
		})
	}
}

func TestEgressUnsafeDestinations(t *testing.T) {
	grant, err := EgressRules("10.42.40.25/24", "10.42.40.1/24", "public", []netip.Prefix{netip.MustParsePrefix("11.12.13.0/24")})
	if err != nil {
		t.Fatal(err)
	}
	for _, dest := range []string{"10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "192.0.0.0/24", "198.18.0.0/15", "11.12.13.0/24"} {
		if !strings.Contains(strings.Join(grant.HTTPS, "\n"), `destination address="`+dest+`" drop`) {
			t.Fatalf("destination not denied: %s", dest)
		}
	}
	for _, source := range []string{"10.42.40.0/24", "10.42.40.1/24", "10.42.40.255/24", "::1/24"} {
		if _, err := EgressRules(source, "10.42.40.1/24", "public", nil); err == nil {
			t.Fatalf("accepted source %s", source)
		}
	}
}
