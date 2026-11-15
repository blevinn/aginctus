package gateway

import (
	"context"
	"fmt"
)

// NativeGatewayServiceState is a verified, read-only Incus service status.
// Neither Running nor Stopped implies application readiness.
type NativeGatewayServiceState string

const (
	NativeGatewayServiceRunning NativeGatewayServiceState = "running"
	NativeGatewayServiceStopped NativeGatewayServiceState = "stopped"
)

type NativeGatewayServiceStatus struct {
	Name string `json:"name"`
	State NativeGatewayServiceState `json:"state"`
}

type NativeGatewayStatus struct {
	Project string `json:"project"`
	Services []NativeGatewayServiceStatus `json:"services"`
	AllRunning bool `json:"allRunning"`
	AllStopped bool `json:"allStopped"`
}

// InspectNativeGatewayStatusWithReader verifies both instance identities,
// image aliases, project policy and storage/network attachments and then
// classifies exact lifecycle states. Unknown/transitional state and absent
// services are errors, not a healthy status. No secret data is read or returned.
func InspectNativeGatewayStatusWithReader(ctx context.Context, top NativeGatewayTopology, devices NativeGatewayDevices, pair NativeGatewayInstancePair, reader NativeGatewayStartupReader) (NativeGatewayStatus,error) {
	if err:=ctx.Err();err!=nil{return NativeGatewayStatus{},err}
	expected,err:=PlanNativeGatewayInstances(top,devices,pair.Postgres.ImageAlias,pair.LiteLLM.ImageAlias)
	if err!=nil || !sameGatewayPair(expected,pair) {
		return NativeGatewayStatus{},fmt.Errorf("native gateway status intent not derived from validated topology")
	}
	if err:=ObserveNativeGatewayAttachmentsWithReader(ctx,top,devices,reader);err!=nil{
		return NativeGatewayStatus{},fmt.Errorf("native gateway status attachments unverified")
	}
	obs,err:=ObserveNativeGatewayInstancesWithReader(ctx,pair,reader)
	if err!=nil || len(obs)!=2 || obs[0].Status!=NativeGatewayCompatible || obs[1].Status!=NativeGatewayCompatible{
		return NativeGatewayStatus{},fmt.Errorf("native gateway status instance identity unverified")
	}
	result:=NativeGatewayStatus{Project:top.Project,AllRunning:true,AllStopped:true}
	for _,spec:=range []NativeGatewayInstanceSpec{pair.Postgres,pair.LiteLLM}{
		if err:=ctx.Err();err!=nil{return NativeGatewayStatus{},err}
		instance,_,err:=reader.GetInstance(spec.Name)
		if err!=nil || instance==nil || instance.Name!=spec.Name || instance.Project!=spec.Project ||
			instance.Config["user.aginctus.resource"]!="gateway-instance" ||
			instance.Config["user.aginctus.role"]!=spec.Role {
			return NativeGatewayStatus{},fmt.Errorf("native gateway status changed during observation")
		}
		state:=NativeGatewayServiceState("")
		switch instance.Status{
		case "Running":state=NativeGatewayServiceRunning;result.AllStopped=false
		case "Stopped":state=NativeGatewayServiceStopped;result.AllRunning=false
		default:return NativeGatewayStatus{},fmt.Errorf("native gateway status lifecycle unverified")
		}
		result.Services=append(result.Services,NativeGatewayServiceStatus{Name:spec.Name,State:state})
	}
	return result,ctx.Err()
}
