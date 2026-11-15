package gateway

import (
	"context"
	"fmt"

	"github.com/lxc/incus/v7/shared/api"
)

// NativeGatewayStartupReader supports only reads of the selected project's
// two owned instances, image identities, network and persistent storage.
type NativeGatewayStartupReader interface {
	NativeGatewayInstanceReader
	NativeGatewayAttachmentReader
}

// NativeGatewayStartupInspection is a redacted, read-only report. Order is the
// dependency order to be used by a future executor after separate authority,
// secret-delivery and service-readiness gates. It is not permission to start.
type NativeGatewayStartupInspection struct {
	Project string `json:"project"`
	Order []string `json:"order"`
	Missing []string `json:"missing,omitempty"`
	RuntimeKeysPresent bool `json:"runtimeKeysPresent"`
}

// InspectNativeGatewayStartupWithReader verifies the complete topology,
// network/storage attachments, immutable project image observations, two
// stopped owned instances and legacy runtime key presence in that order.
// No secret values are returned or logged. The result is NOT an activation
// grant, even when RuntimeKeysPresent is true.
func InspectNativeGatewayStartupWithReader(ctx context.Context,top NativeGatewayTopology,devices NativeGatewayDevices,pair NativeGatewayInstancePair,r NativeGatewayStartupReader)(NativeGatewayStartupInspection,error) {
	if err:=ctx.Err();err!=nil{return NativeGatewayStartupInspection{},err}
	expected,err:=PlanNativeGatewayInstances(top,devices,pair.Postgres.ImageAlias,pair.LiteLLM.ImageAlias)
	if err!=nil || !sameGatewayPair(expected,pair) {
		return NativeGatewayStartupInspection{},fmt.Errorf("native gateway startup plan identity unverified")
	}
	if err:=ObserveNativeGatewayAttachmentsWithReader(ctx,top,devices,r);err!=nil {
		return NativeGatewayStartupInspection{},fmt.Errorf("native gateway startup attachment preflight failed: %w",err)
	}
	obs,err:=ObserveNativeGatewayInstancesWithReader(ctx,pair,r)
	if err!=nil || len(obs)!=2 || obs[0].Status!=NativeGatewayCompatible || obs[1].Status!=NativeGatewayCompatible {
		return NativeGatewayStartupInspection{},fmt.Errorf("native gateway startup instances not safely observed")
	}
	instances:=make([]*api.Instance,0,2)
	for _,spec:=range []NativeGatewayInstanceSpec{pair.Postgres,pair.LiteLLM} {
		if err:=ctx.Err();err!=nil{return NativeGatewayStartupInspection{},err}
		inst,_,err:=r.GetInstance(spec.Name)
		if err!=nil || inst==nil || inst.Name!=spec.Name {
			return NativeGatewayStartupInspection{},fmt.Errorf("native gateway startup runtime instance unavailable")
		}
		instances=append(instances,inst)
	}
	assessment,err:=AssessNativeGatewayRuntimeConfig(ctx,pair,instances)
	if err!=nil{return NativeGatewayStartupInspection{},err}
	return NativeGatewayStartupInspection{
		Project:top.Project,Order:[]string{pair.Postgres.Name,pair.LiteLLM.Name},
		Missing:assessment.Missing,RuntimeKeysPresent:assessment.Complete,
	},ctx.Err()
}
