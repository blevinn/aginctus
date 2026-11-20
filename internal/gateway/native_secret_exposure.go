package gateway

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/lxc/incus/v7/shared/api"
)

// NativeGatewaySecretExposure is an observation of potentially sensitive
// environment keys already persisted in Incus. Values are never returned.
// It is NOT a credential attestation, authorization, or startup capability.
type NativeGatewaySecretExposure struct {
	Project string   `json:"project"`
	ExposedKeys []string `json:"exposedKeys,omitempty"`
	UnresolvedKeys []string `json:"unresolvedKeys,omitempty"`
	SafeForNativeStart bool `json:"safeForNativeStart"`
}

// AuditNativeGatewaySecretExposure examines only the sensitive keys in the
// established runtime contract, never copying their contents to diagnostics.
// Incus instance configuration is visible to principals allowed to read it;
// legacy environment-value presence does not justify native startup.
// This function is read-only, and does not claim that a missing key proves a
// secure replacement was delivered.
func AuditNativeGatewaySecretExposure(ctx context.Context, pair NativeGatewayInstancePair, instances []*api.Instance) (NativeGatewaySecretExposure, error) {
	if err := ctx.Err(); err != nil { return NativeGatewaySecretExposure{},err }
	if len(instances)!=2 || validateNativeGatewayPairOwner(pair)!=nil {
		return NativeGatewaySecretExposure{},fmt.Errorf("gateway credential exposure requires exact service pair")
	}
	for index,spec := range []NativeGatewayInstanceSpec{pair.Postgres,pair.LiteLLM} {
		inst:=instances[index]
		if inst==nil || inst.Name!=spec.Name || inst.Project!=spec.Project ||
			inst.Config["user.aginctus.managed"]!="true" ||
			inst.Config["user.aginctus.resource"]!="gateway-instance" ||
			inst.Config["user.aginctus.role"]!=spec.Role ||
			inst.Config["user.aginctus.deployment"]!=spec.Project ||
			!gatewayOwnerMatches(inst.Config,spec.EnvironmentID) {
			return NativeGatewaySecretExposure{},fmt.Errorf("gateway credential exposure requires owned service instances")
		}
	}
	result:=NativeGatewaySecretExposure{Project:pair.Postgres.Project}
	for _,requirement:=range NativeGatewayRuntimeRequirements(){
		if !requirement.Sensitive {continue}
		index:=0
		if requirement.Service=="litellm" {index=1}
		key:="environment."+requirement.Key
		value,present:=instances[index].Config[key]
		if !present {continue}
		result.ExposedKeys=append(result.ExposedKeys,requirement.Service+"."+requirement.Key)
		if strings.Contains(value,"${") || strings.TrimSpace(value)=="" {
			result.UnresolvedKeys=append(result.UnresolvedKeys,requirement.Service+"."+requirement.Key)
		}
	}
	slices.Sort(result.ExposedKeys)
	slices.Sort(result.UnresolvedKeys)
	// This audit deliberately never confers startup permission. Even when no
	// sensitive keys were exposed, a separate trusted credential-delivery
	// attestation, accepted revision and writer fence are still required.
	result.SafeForNativeStart=false
	return result,ctx.Err()
}
