package gateway

import (
	"testing"

	"github.com/lxc/incus/v7/shared/api"
)

func TestVerifyPersistedEgressNICs(t *testing.T) {
	spec := egressTestSpec()
	spec.EgressIPv4Address = "10.42.40.25/24"
	good := func() *api.Instance {
		return &api.Instance{
			Name: "litellm-1", Status: "Stopped",
			InstancePut: api.InstancePut{
				Config: map[string]string{
					"user.aginctus.managed": "true",
					"user.aginctus.resource": "compose-instance",
					"user.aginctus.deployment": spec.Project,
				},
				Devices: map[string]map[string]string{
					"eth0": {"type": "nic", "network": spec.Network, "ipv4.gateway": "none"},
					"eth1": {"type": "nic", "network": spec.EgressNetwork, "ipv4.address": spec.EgressIPv4Address},
				},
			},
		}
	}
	if err := verifyPersistedEgressNICs(good(), spec, "eth0", "eth1"); err != nil {
		t.Fatalf("persisted NIC state rejected: %v", err)
	}
	for _, tc := range []struct {
		name string
		change func(*api.Instance)
	}{
		{"running", func(i *api.Instance) { i.Status = "Running" }},
		{"foreign", func(i *api.Instance) { i.Config["user.aginctus.managed"] = "false" }},
		{"management gateway", func(i *api.Instance) { delete(i.Devices["eth0"], "ipv4.gateway") }},
		{"egress address", func(i *api.Instance) { i.Devices["eth1"]["ipv4.address"] = "10.42.40.30/24" }},
		{"egress NIC missing", func(i *api.Instance) { delete(i.Devices, "eth1") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			instance := good()
			tc.change(instance)
			if err := verifyPersistedEgressNICs(instance, spec, "eth0", "eth1"); err == nil {
				t.Fatal("post-update drift was accepted")
			}
		})
	}
	if err := verifyPersistedEgressNICs(nil, spec, "eth0", "eth1"); err == nil {
		t.Fatal("nil instance was accepted")
	}
}
