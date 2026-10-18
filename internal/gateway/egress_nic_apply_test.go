package gateway

import (
	"context"
	"fmt"
	"strings"
	"testing"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

type fakeStoppedNICEditor struct {
	instance *api.Instance
	etag     string
	writes   int
}

func (e *fakeStoppedNICEditor) GetInstance(string) (*api.Instance, string, error) {
	return e.instance, e.etag, nil
}
func (e *fakeStoppedNICEditor) UpdateInstance(string, api.InstancePut, string) (incusclient.Operation, error) {
	e.writes++
	return nil, fmt.Errorf("test update intentionally disabled")
}
func TestStoppedEgressNICReconciliationGuards(t *testing.T) {
	spec := egressTestSpec()
	spec.EgressIPv4Address = "10.42.40.25/24"
	read := func(_ context.Context, args ...string) ([]byte, error) {
		switch args[len(args)-1] {
		case "/1.0/networks/aginctus-egress":
			return []byte(`{"name":"aginctus-egress","type":"bridge","managed":true,"config":{"user.aginctus.managed":"true","user.aginctus.resource":"gateway-egress-network","ipv4.address":"10.42.40.1/24","ipv4.nat":"true","ipv4.routing":"true","ipv4.dhcp.ranges":"10.42.40.100-10.42.40.199"}}`), nil
		case "/1.0/networks/aginctus-egress/leases":
			return []byte(`[]`), nil
		case "/1.0/projects?recursion=1":
			return []byte(`[{"name":"aginctus-gateway"}]`), nil
		case "/1.0/instances?recursion=2&project=aginctus-gateway":
			return []byte(`[{"name":"litellm-1","config":{"user.aginctus.managed":"true","user.aginctus.resource":"compose-instance","user.aginctus.deployment":"aginctus-gateway"},"expanded_devices":{"eth1":{"type":"nic","network":"aginctus-egress"}}}]`), nil
		case "/1.0/instances/litellm-1?project=aginctus-gateway&recursion=1":
			return []byte(`{"name":"litellm-1","config":{"user.aginctus.managed":"true","user.aginctus.resource":"compose-instance","user.aginctus.deployment":"aginctus-gateway"},"devices":{"eth0":{"type":"nic","network":"aginctus-mgmt"},"eth1":{"type":"nic","network":"aginctus-egress"}},"expanded_devices":{"eth0":{"type":"nic","network":"aginctus-mgmt"},"eth1":{"type":"nic","network":"aginctus-egress"}}}`), nil
		default:
			return nil, fmt.Errorf("unexpected query: %v", args)
		}
	}
	newEditor := func() *fakeStoppedNICEditor {
		return &fakeStoppedNICEditor{etag: "etag", instance: &api.Instance{
			Name: "litellm-1", Status: "Stopped",
			InstancePut: api.InstancePut{
				Config: map[string]string{"user.aginctus.managed": "true", "user.aginctus.resource": "compose-instance", "user.aginctus.deployment": "aginctus-gateway"},
				Devices: map[string]map[string]string{
					"eth0": {"type": "nic", "network": "aginctus-mgmt"},
					"eth1": {"type": "nic", "network": "aginctus-egress"},
				},
			},
			ExpandedDevices: map[string]map[string]string{
				"eth0": {"type": "nic", "network": "aginctus-mgmt"},
				"eth1": {"type": "nic", "network": "aginctus-egress"},
			},
		}}
	}
	for _, tc := range []struct{ name, issue, want string }{
		{"running", "running", "already be stopped"},
		{"etag missing", "etag", "ETag"},
		{"foreign", "foreign", "ownership"},
		{"conflicting IP", "address", "conflicting"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			editor := newEditor()
			switch tc.issue {
			case "running":
				editor.instance.Status = "Running"
			case "etag":
				editor.etag = ""
			case "foreign":
				editor.instance.Config["user.aginctus.managed"] = "false"
			case "address":
				editor.instance.Devices["eth1"]["ipv4.address"] = "10.42.40.30/24"
			}
			err := reconcileStoppedEgressNICs(context.Background(), spec, read, editor)
			if err == nil || !strings.Contains(err.Error(), tc.want) || editor.writes != 0 {
				t.Fatalf("expected %s, no writes: err=%v writes=%d", tc.want, err, editor.writes)
			}
		})
	}
}
