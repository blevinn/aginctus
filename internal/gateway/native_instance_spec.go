package gateway

import (
	"fmt"
	"maps"
	"slices"
)

// NativeGatewayInstanceSpec is a pure description of one proposed Incus
// service instance. It is not an accepted deployment revision, write grant,
// secret material, or an API request ready to execute.
type NativeGatewayInstanceSpec struct {
	Project string
	Name string
	Role string
	ImageAlias string
	DependsOn []string
	Devices map[string]map[string]string
	SecretEnvironment []string
}

// NativeGatewayInstancePair describes both services in dependency order.
// Both instances remain unstarted until a separately authorized executor
// has established storage, runtime configuration, and readiness policy.
type NativeGatewayInstancePair struct {
	Postgres NativeGatewayInstanceSpec
	LiteLLM NativeGatewayInstanceSpec
}

// PlanNativeGatewayInstances binds the previously validated topology and
// devices to explicitly selected project-local image aliases. Neither aliases
// nor guest instances are created here. No secret values enter the plan.
func PlanNativeGatewayInstances(topology NativeGatewayTopology, devices NativeGatewayDevices, postgresAlias, liteLLMAlias string) (NativeGatewayInstancePair, error) {
	if err := topology.ValidateExecutionShape(); err != nil {
		return NativeGatewayInstancePair{}, err
	}
	if devices.Project != topology.Project || !nativeDeviceName.MatchString(postgresAlias) ||
		!nativeDeviceName.MatchString(liteLLMAlias) || postgresAlias == liteLLMAlias {
		return NativeGatewayInstancePair{}, fmt.Errorf("native gateway instance project or image alias invalid")
	}
	// Prevent a caller from injecting arbitrary host attachments by passing an
	// untrusted devices struct. Rebuild the exact expected plan with the
	// explicitly supplied pool names, rather than inferring authority from the
	// contents of the maps.
	pgRoot, pgData := devices.Postgres["root"], devices.Postgres["database"]
	liteRoot := devices.LiteLLM["root"]
	if pgRoot == nil || pgData == nil || liteRoot == nil ||
		pgRoot["pool"] == "" || pgData["pool"] == "" || pgRoot["pool"] != liteRoot["pool"] {
		return NativeGatewayInstancePair{}, fmt.Errorf("native gateway instance storage plan missing")
	}
	expected, err := PlanNativeGatewayDevices(topology, pgRoot["pool"], pgData["pool"])
	if err != nil || !equalNativeDevices(devices.Postgres,expected.Postgres) ||
		!equalNativeDevices(devices.LiteLLM,expected.LiteLLM) {
		return NativeGatewayInstancePair{}, fmt.Errorf("native gateway instance devices do not match validated topology")
	}
	clone := func(src map[string]map[string]string) map[string]map[string]string {
		dst:=make(map[string]map[string]string,len(src))
		for k,v:=range src { dst[k]=maps.Clone(v) }
		return dst
	}
	return NativeGatewayInstancePair{
		Postgres:NativeGatewayInstanceSpec{
			Project:topology.Project,Name:"postgres-1",Role:"postgres",ImageAlias:postgresAlias,
			Devices:clone(expected.Postgres),
			SecretEnvironment:slices.Clone(topology.Services[0].SecretEnvironment),
		},
		LiteLLM:NativeGatewayInstanceSpec{
			Project:topology.Project,Name:"litellm-1",Role:"litellm",ImageAlias:liteLLMAlias,
			Devices:clone(expected.LiteLLM),
			DependsOn:slices.Clone(topology.Services[1].DependsOn),
			SecretEnvironment:slices.Clone(topology.Services[1].SecretEnvironment),
		},
	},nil
}

func equalNativeDevices(a,b map[string]map[string]string) bool {
	if len(a)!=len(b) {return false}
	for name,expected:=range b { if !maps.Equal(a[name],expected) {return false} }
	return true
}
