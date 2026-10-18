package gateway

import (
	"context"
	"strings"
	"testing"
)

func TestAuditEgressInstances(t *testing.T) {
	for _, tc := range []struct {
		name string
		instances string
		want string
	}{
		{"assigned", `[{"expanded_devices":{"eth1":{"type":"nic","network":"aginctus-egress","ipv4.address":"10.42.40.254"}}}]`, "10.42.40.254"},
		{"dynamic", `[{"expanded_devices":{"eth1":{"type":"nic","network":"aginctus-egress"}}}]`, "blocked"},
		{"missing expanded", `[{"devices":{}}]`, "blocked"},
		{"different network", `[{"expanded_devices":{"eth0":{"type":"nic","network":"other"}}}]`, "none"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := func(_ context.Context, args ...string) ([]byte, error) {
				switch args[len(args)-1] {
				case "/1.0/projects?recursion=1":
					return []byte(`[{"name":"default"}]`), nil
				case "/1.0/instances?recursion=2&project=default":
					return []byte(tc.instances), nil
				default:
					t.Fatalf("unexpected inventory request %v", args)
					return nil, nil
				}
			}
			got, err := auditEgressInstances(context.Background(), "aginctus-egress", run)
			if tc.want == "blocked" {
				if err == nil { t.Fatal("expected blocked inventory") }
			} else if err != nil {
				t.Fatal(err)
			} else if tc.want == "none" && len(got) != 0 {
				t.Fatalf("unexpected assignment %v", got)
			} else if tc.want != "none" && (len(got) != 1 || !strings.Contains(got[0].String(), tc.want)) {
				t.Fatalf("unexpected assignment %v", got)
			}
		})
	}
}
