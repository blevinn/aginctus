package gateway

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/lxc/incus/v7/shared/api"
)

// NativeGatewayRuntimeRequirement describes a key required by the historical
// Compose service contract. It does NOT prescribe a secret-delivery mechanism.
// The native gateway must not start until a separately reviewed credential
// handoff and accepted-state authorization have been implemented.
type NativeGatewayRuntimeRequirement struct {
	Service string `json:"service"`
	Key string `json:"key"`
	Sensitive bool `json:"sensitive"`
}

// NativeGatewayRuntimeRequirements lists only names, never runtime values.
// DATABASE_URL embeds the PostgreSQL password and is itself sensitive.
func NativeGatewayRuntimeRequirements() []NativeGatewayRuntimeRequirement {
	return []NativeGatewayRuntimeRequirement{
		{Service:"postgres",Key:"POSTGRES_DB"},
		{Service:"postgres",Key:"POSTGRES_USER"},
		{Service:"postgres",Key:"POSTGRES_PASSWORD",Sensitive:true},
		{Service:"litellm",Key:"DATABASE_URL",Sensitive:true},
		{Service:"litellm",Key:"LITELLM_MASTER_KEY",Sensitive:true},
		{Service:"litellm",Key:"LITELLM_SALT_KEY",Sensitive:true},
		{Service:"litellm",Key:"STORE_MODEL_IN_DB"},
	}
}

// NativeGatewayRuntimeAssessment is a redacted, observation-only inventory.
// Complete only means required names are populated in the observed Incus
// config. It neither establishes their provenance nor permits a start.
type NativeGatewayRuntimeAssessment struct {
	Project string `json:"project"`
	Missing []string `json:"missing,omitempty"`
	Complete bool `json:"complete"`
}

// AssessNativeGatewayRuntimeConfig verifies required runtime environment keys
// exist in the two explicitly owned STOPPED containers. Secrets never enter
// results or errors. It refuses unresolved template placeholders. The use of
// environment values here is an observation of existing legacy-style settings,
// NOT an endorsement of persisting native secret values in Incus config.
// This function cannot authorize activation, even on a Complete result.
func AssessNativeGatewayRuntimeConfig(ctx context.Context,pair NativeGatewayInstancePair,instances []*api.Instance)(NativeGatewayRuntimeAssessment,error){
	if err:=ctx.Err();err!=nil{return NativeGatewayRuntimeAssessment{},err}
	if len(instances)!=2 || validateNativeGatewayPairOwner(pair)!=nil {
		return NativeGatewayRuntimeAssessment{},fmt.Errorf("native gateway runtime assessment requires exact service pair")
	}
	out:=NativeGatewayRuntimeAssessment{Project:pair.Postgres.Project}
	for idx,role:=range []string{"postgres","litellm"} {
		inst:=instances[idx]
		spec:=pair.Postgres
		if idx==1 {spec=pair.LiteLLM}
		if inst==nil || inst.Name!=spec.Name || inst.Project!=spec.Project ||
			inst.Status!="Stopped" || inst.Config["user.aginctus.managed"]!="true" ||
			inst.Config["user.aginctus.resource"]!="gateway-instance" ||
			inst.Config["user.aginctus.role"]!=role ||
			inst.Config["user.aginctus.deployment"]!=spec.Project ||
			!gatewayOwnerMatches(inst.Config,spec.EnvironmentID) {
			return NativeGatewayRuntimeAssessment{},fmt.Errorf("native gateway runtime instance ownership or stopped state unverified")
		}
	}
	for _,requirement:=range NativeGatewayRuntimeRequirements(){
		index:=0
		if requirement.Service=="litellm" {index=1}
		value:=instances[index].Config["environment."+requirement.Key]
		if strings.TrimSpace(value)=="" || strings.Contains(value,"${") {
			out.Missing=append(out.Missing,requirement.Service+"."+requirement.Key)
		}
	}
	slices.Sort(out.Missing)
	out.Complete=len(out.Missing)==0
	return out,ctx.Err()
}
