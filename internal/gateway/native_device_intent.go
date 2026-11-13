package gateway

import (
	"fmt"
	"net/netip"
	"regexp"
)

var nativeDeviceName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// NativeGatewayDevices is a pure description of devices needed to construct
// exactly the two gateway instances. It neither creates nor attaches anything.
// The project and pool must be independently authorized before execution.
type NativeGatewayDevices struct {
	Project string
	Postgres map[string]map[string]string
	LiteLLM map[string]map[string]string
}

// PlanNativeGatewayDevices renders an explicit, narrow attachment set after
// full topology validation. Persistent database storage is mounted only by
// Postgres; only LiteLLM is connected to the separate egress bridge.
// Root and database pools are selected by the caller, never inferred from
// the host's default project.
func PlanNativeGatewayDevices(topology NativeGatewayTopology, rootPool, databasePool string) (NativeGatewayDevices, error) {
	if err := topology.ValidateExecutionShape(); err != nil {
		return NativeGatewayDevices{}, err
	}
	if !nativeDeviceName.MatchString(topology.Project) || !nativeDeviceName.MatchString(topology.Deployment) ||
		!nativeDeviceName.MatchString(topology.DatabaseVolume) ||
		!nativeDeviceName.MatchString(rootPool) || !nativeDeviceName.MatchString(databasePool) ||
		!nativeDeviceName.MatchString(topology.ManagementNetwork) ||
		(topology.EgressNetwork != "" && !nativeDeviceName.MatchString(topology.EgressNetwork)) {
		return NativeGatewayDevices{}, fmt.Errorf("native gateway devices require explicit safe pool and network names")
	}
	postgres := map[string]map[string]string{
		"root": {"type":"disk", "path":"/", "pool":rootPool},
		"management": {"type":"nic", "network":topology.ManagementNetwork, "name":"eth0", "ipv4.gateway":"none"},
		"database": {"type":"disk", "pool":databasePool, "source":topology.DatabaseVolume, "path":"/var/lib/postgresql/data"},
	}
	litellm := map[string]map[string]string{
		"root": {"type":"disk", "path":"/", "pool":rootPool},
		"management": {"type":"nic", "network":topology.ManagementNetwork, "name":"eth0", "ipv4.gateway":"none"},
	}
	if topology.EgressNetwork != "" {
		prefix, err := netip.ParsePrefix(topology.Services[1].Networks[1].IPv4Address)
		if err != nil {
			return NativeGatewayDevices{}, fmt.Errorf("native gateway egress address rejected")
		}
		litellm["egress"] = map[string]string{
			"type":"nic", "network":topology.EgressNetwork, "name":"eth1",
			"ipv4.address":prefix.Addr().String(),
		}
	}
	return NativeGatewayDevices{Project:topology.Project,Postgres:postgres,LiteLLM:litellm},nil
}
