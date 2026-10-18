package gateway

import (
	"context"
	"strings"
	"testing"
)

func TestPlanEgressDHCPMigration(t *testing.T) {
	for _, tc := range []struct {
		name, ranges, leases, want string
	}{
		{"preserves leases", "10.42.40.100-10.42.40.199", `[{"address":"10.42.40.110"}]`, ""},
		{"excludes lease", "10.42.40.100-10.42.40.199", `[{"address":"10.42.40.80"}]`, "excludes"},
		{"overlapping static", "10.42.40.20-10.42.40.30", `[]`, "overlaps"},
		{"malformed", "invalid", `[]`, "DHCP pool"},
		{"missing inventory", "10.42.40.100-10.42.40.199", "null", "inventory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := egressTestSpec()
			spec.EgressIPv4Address = "10.42.40.25/24"
			query := func(_ context.Context, args ...string) ([]byte, error) {
				switch args[len(args)-1] {
				case "/1.0/networks/aginctus-egress":
					return []byte(`{"name":"aginctus-egress","type":"bridge","managed":true,"config":{"user.aginctus.managed":"true","user.aginctus.resource":"gateway-egress-network","ipv4.address":"10.42.40.1/24","ipv4.nat":"true","ipv4.routing":"true","ipv6.address":"none"}}`), nil
				case "/1.0/networks/aginctus-egress/leases":
					return []byte(tc.leases), nil
				default:
					t.Fatalf("unexpected query: %v", args)
					return nil, nil
				}
			}
			plan, err := PlanEgressDHCPMigration(context.Background(), spec, tc.ranges, query)
			if tc.want == "" {
				if err != nil || !strings.Contains(plan, "READ-ONLY") {
					t.Fatalf("expected safe migration plan, got %q: %v", plan, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %q, %v", tc.want, plan, err)
			}
		})
	}
}
