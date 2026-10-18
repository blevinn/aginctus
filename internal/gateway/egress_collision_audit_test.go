package gateway

import (
	"context"
	"net/netip"
	"strings"
	"testing"
)

func TestAuditRequestedEgressAddress(t *testing.T) {
	for _, tc := range []struct {
		name, leases, otherIP, want string
	}{
		{"available", "[]", "10.42.40.24/24", ""},
		{"leased", `[{"address":"10.42.40.25"}]`, "10.42.40.24/24", "existing lease"},
		{"assigned", "[]", "10.42.40.25/24", "already assigned"},
		{"opaque NIC", "[]", "", "no explicit"},
		{"missing leases", "null", "10.42.40.24/24", "lease inventory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := func(_ context.Context, args ...string) ([]byte, error) {
				switch args[len(args)-1] {
				case "/1.0/networks/aginctus-egress/leases":
					return []byte(tc.leases), nil
				case "/1.0/projects?recursion=1":
					return []byte(`[{"name":"aginctus-gateway"},{"name":"other"}]`), nil
				case "/1.0/instances?recursion=2&project=aginctus-gateway":
					return []byte(`[{"name":"litellm-1","config":{"user.aginctus.managed":"true","user.aginctus.resource":"compose-instance","user.aginctus.deployment":"aginctus-gateway"},"expanded_devices":{"eth1":{"type":"nic","network":"aginctus-egress"}}}]`), nil
				case "/1.0/instances?recursion=2&project=other":
					return []byte(`[{"name":"other-instance","expanded_devices":{"eth0":{"type":"nic","network":"aginctus-egress","ipv4.address":"`+tc.otherIP+`"}}}]`), nil
				default:
					t.Fatalf("unexpected query: %v", args)
					return nil, nil
				}
			}
			err := auditRequestedEgressAddress(context.Background(), "aginctus-egress", "aginctus-gateway", mustParseAddress(t, "10.42.40.25"), query)
			if tc.want == "" {
				if err != nil { t.Fatal(err) }
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func mustParseAddress(t *testing.T, value string) netip.Addr {
	t.Helper()
	addr, err := netip.ParseAddr(value)
	if err != nil { t.Fatal(err) }
	return addr
}
