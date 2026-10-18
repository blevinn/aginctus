package gateway

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestEgressPreflightExcludesDHCPPool(t *testing.T) {
	run := func(_ context.Context, args ...string) ([]byte, error) {
		switch args[len(args)-1] {
		case "/1.0/networks/aginctus-egress":
			return []byte(`{"name":"aginctus-egress","type":"bridge","managed":true,"config":{"user.aginctus.managed":"true","user.aginctus.resource":"gateway-egress-network","ipv4.nat":"true","ipv4.routing":"true","ipv6.address":"none","ipv4.dhcp":"true","ipv4.dhcp.ranges":"10.42.40.200-10.42.40.254","ipv4.address":"10.42.40.1/24"}}`), nil
		case "/1.0/networks/aginctus-egress/leases":
			return []byte("[]"), nil
		case "/1.0/projects?recursion=1":
			return []byte(`[{"name":"default"}]`), nil
		case "/1.0/instances?recursion=2&project=default":
			return []byte(`[]`), nil
		default:
			t.Fatalf("unexpected API request %v", args)
			return nil, nil
		}
	}
	got, err := egressAddressPreflight(context.Background(), "aginctus-egress", run)
	if got != "" || err == nil || !strings.Contains(err.Error(), "10.42.40.199/24") ||
		!strings.Contains(err.Error(), "unreserved") {
		t.Fatalf("unexpected result %q, %v", got, err)
	}
}

func TestEgressAddressPreflightFailClosed(t *testing.T) {
	network := func(dhcp, owner string) []byte {
		return []byte(fmt.Sprintf(`{"name":"aginctus-egress","type":"bridge","managed":true,"config":{"user.aginctus.managed":"true","user.aginctus.resource":%q,"ipv4.nat":"true","ipv4.routing":"true","ipv6.address":"none","ipv4.dhcp":%q,"ipv4.address":"10.42.40.1/24"}}`, owner, dhcp))
	}
	for _, tc := range []struct {
		name, dhcp, owner, leases, want string
	}{
		{"default DHCP", "", "gateway-egress-network", "[]", "DHCP"},
		{"enabled DHCP", "true", "gateway-egress-network", "[]", "DHCP"},
		{"foreign bridge", "false", "foreign", "[]", "ownership"},
		{"malformed leases", "false", "gateway-egress-network", "{", "decode leases"},
		{"incomplete lease", "false", "gateway-egress-network", `[{"address":""}]`, "incomplete"},
		{"unreserved candidate", "false", "gateway-egress-network", "[]", "unreserved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			run := func(_ context.Context, args ...string) ([]byte, error) {
				calls++
				switch args[len(args)-1] {
				case "/1.0/networks/aginctus-egress":
					return network(tc.dhcp, tc.owner), nil
				case "/1.0/networks/aginctus-egress/leases":
					return []byte(tc.leases), nil
				case "/1.0/projects?recursion=1":
					return []byte(`[{"name":"default"}]`), nil
				case "/1.0/instances?recursion=2&project=default":
					return []byte(`[]`), nil
				default:
					t.Fatalf("unexpected Incus query: %v", args)
					return nil, nil
				}
			}
			candidate, err := egressAddressPreflight(context.Background(), "aginctus-egress", run)
			if candidate != "" || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("candidate=%q, err=%v; expected %q", candidate, err, tc.want)
			}
			if tc.dhcp != "false" || tc.owner != "gateway-egress-network" {
				if calls != 1 {
					t.Fatalf("unsafe bridge caused %d Incus queries; expected one", calls)
				}
			}
		})
	}
}
