package gateway

import (
	"context"
	"strings"
	"testing"
)

func TestPlanEgressReconciliation(t *testing.T) {
	for _, tc := range []struct {
		name, dhcp, owner, host, want string
	}{
		{"owned, disabled DHCP", "false", "true", "", "would set"},
		{"unrestricted DHCP", "", "true", "", "DHCP"},
		{"foreign instance", "false", "false", "", "ownership"},
		{"existing conflicting address", "false", "true", "10.42.40.20/24", "conflicting"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := egressTestSpec()
			spec.EgressIPv4Address = "10.42.40.25/24"
			query := func(_ context.Context, args ...string) ([]byte, error) {
				switch args[len(args)-1] {
				case "/1.0/networks/aginctus-egress":
					return []byte(`{"name":"aginctus-egress","type":"bridge","managed":true,"config":{"user.aginctus.managed":"true","user.aginctus.resource":"gateway-egress-network","ipv4.address":"10.42.40.1/24","ipv4.nat":"true","ipv4.routing":"true","ipv4.dhcp":"` + tc.dhcp + `"}}`), nil
				case "/1.0/networks/aginctus-egress/leases":
					return []byte(`[]`), nil
				case "/1.0/projects?recursion=1":
					return []byte(`[{"name":"aginctus-gateway"}]`), nil
				case "/1.0/instances?recursion=2&project=aginctus-gateway":
					return []byte(`[{"name":"litellm-1","config":{"user.aginctus.managed":"true","user.aginctus.resource":"compose-instance","user.aginctus.deployment":"aginctus-gateway"},"expanded_devices":{"eth1":{"type":"nic","network":"aginctus-egress"}}}]`), nil
				case "/1.0/instances/litellm-1?project=aginctus-gateway&recursion=1":
					return []byte(`{"name":"litellm-1","config":{"user.aginctus.managed":"` + tc.owner + `","user.aginctus.resource":"compose-instance","user.aginctus.deployment":"aginctus-gateway"},"devices":{"eth0":{"type":"nic","network":"aginctus-mgmt"},"eth1":{"type":"nic","network":"aginctus-egress","ipv4.address":"` + tc.host + `"}},"expanded_devices":{"eth0":{"type":"nic","network":"aginctus-mgmt"},"eth1":{"type":"nic","network":"aginctus-egress"}}}`), nil
				default:
					t.Fatalf("unexpected query %v", args)
					return nil, nil
				}
			}
			plan, err := PlanEgressReconciliation(context.Background(), spec, query)
			if tc.want == "would set" {
				if err != nil || !strings.Contains(plan, tc.want) {
					t.Fatalf("unexpected plan %q: %v", plan, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %q, %v", tc.want, plan, err)
			}
		})
	}
}
