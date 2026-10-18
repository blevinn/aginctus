package gateway

import (
	"context"
	"strings"
	"testing"
)

func TestAuditEgressRuntime(t *testing.T) {
	spec := egressTestSpec()
	for _, tc := range []struct {
		name, managementGateway, address, owner, expected string
	}{
		{"valid", "none", "10.42.40.254/24", "true", ""},
		{"missing egress address", "none", "", "true", "static IPv4"},
		{"management routing", "", "10.42.40.254/24", "true", "management"},
		{"foreign instance", "none", "10.42.40.254/24", "false", "ownership"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := `{"name":"litellm-1","config":{"user.aginctus.managed":"` + tc.owner + `","user.aginctus.resource":"compose-instance","user.aginctus.deployment":"aginctus-gateway"},"expanded_devices":{"eth0":{"type":"nic","network":"aginctus-mgmt","ipv4.gateway":"` + tc.managementGateway + `"},"eth1":{"type":"nic","network":"aginctus-egress","ipv4.address":"` + tc.address + `"}}}`
			call := func(_ context.Context, args ...string) ([]byte, error) { return []byte(payload), nil }
			err := AuditEgressRuntime(context.Background(), spec, call)
			if tc.expected == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.expected) {
				t.Fatalf("expected %q, got %v", tc.expected, err)
			}
		})
	}
}
