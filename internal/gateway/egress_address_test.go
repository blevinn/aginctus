package gateway

import (
	"net/netip"
	"testing"
)

func TestSelectEgressIPv4(t *testing.T) {
	got, err := selectEgressIPv4("10.42.40.1/24", []netip.Addr{
		netip.MustParseAddr("10.42.40.254"),
		netip.MustParseAddr("10.42.40.252"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "10.42.40.253/24" {
		t.Fatalf("selected %q", got)
	}
}

func TestSelectEgressIPv4FailClosed(t *testing.T) {
	for _, tc := range []struct {
		name string
		cidr string
		excluded []netip.Addr
	}{
		{"invalid", "auto", nil},
		{"ipv6", "2001:db8::1/64", nil},
		{"too large", "10.0.0.1/8", nil},
		{"gateway network", "10.42.40.0/24", nil},
		{"outside exclusion", "10.42.40.1/24", []netip.Addr{netip.MustParseAddr("10.42.41.2")}},
		{"no addresses", "10.42.40.1/30", []netip.Addr{netip.MustParseAddr("10.42.40.2")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := selectEgressIPv4(tc.cidr, tc.excluded); err == nil {
				t.Fatalf("unexpected allocation %s", got)
			}
		})
	}
}
