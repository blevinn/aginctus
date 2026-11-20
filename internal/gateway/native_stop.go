package gateway

import (
	"context"
	"fmt"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

// NativeGatewayStopClient permits observed state transitions, but no instance,
// volume, image, network or credential deletion.
type NativeGatewayStopClient interface {
	NativeGatewayInstanceReader
	NativeGatewayAttachmentReader
	native.InstanceLifecycleClient
}

type nativeGatewayStopSDK struct{ server incusclient.InstanceServer }
func (c nativeGatewayStopSDK) GetConnectionInfo() (*incusclient.ConnectionInfo,error) { return c.server.GetConnectionInfo() }
func (c nativeGatewayStopSDK) GetProject(n string) (*api.Project,string,error) { return c.server.GetProject(n) }
func (c nativeGatewayStopSDK) GetInstance(n string) (*api.Instance,string,error) { return c.server.GetInstance(n) }
func (c nativeGatewayStopSDK) GetImageAlias(n string) (*api.ImageAliasesEntry,string,error) { return c.server.GetImageAlias(n) }
func (c nativeGatewayStopSDK) GetImage(n string) (*api.Image,string,error) { return c.server.GetImage(n) }
func (c nativeGatewayStopSDK) GetNetwork(n string) (*api.Network,string,error) { return c.server.GetNetwork(n) }
func (c nativeGatewayStopSDK) GetStoragePool(n string) (*api.StoragePool,string,error) { return c.server.GetStoragePool(n) }
func (c nativeGatewayStopSDK) GetStoragePoolVolume(p,k,n string) (*api.StorageVolume,string,error) { return c.server.GetStoragePoolVolume(p,k,n) }
func (c nativeGatewayStopSDK) UpdateInstanceState(n string,s api.InstanceStatePut,etag string) (native.InstanceOperation,error) { return c.server.UpdateInstanceState(n,s,etag) }

// NativeGatewayStopResult exposes the sequence of verified no-ops and changes.
// Errors are redacted; the caller must inspect partially completed transitions.
type NativeGatewayStopResult struct {
	Project string `json:"project"`
	Changed []string `json:"changed,omitempty"`
	Verified []string `json:"verified,omitempty"`
}

// StopNativeGatewayWithClient stops only the two exact owned services in reverse
// dependency order: LiteLLM, then PostgreSQL. It never removes any resource,
// detaches a data volume, or forces a stop. No production controller calls this.
// An external accepted-revision check and writer fence must supply guard.
func StopNativeGatewayWithClient(ctx context.Context,top NativeGatewayTopology,devices NativeGatewayDevices,pair NativeGatewayInstancePair,client NativeGatewayStopClient,guard func(context.Context)error) (NativeGatewayStopResult,error) {
	out:=NativeGatewayStopResult{Project:top.Project}
	fail:=func(stage string,err error)(NativeGatewayStopResult,error) {
		return out,fmt.Errorf("native gateway stop %s: %w",stage,err)
	}
	if guard==nil {return fail("authority",fmt.Errorf("required writer authority unavailable"))}
	reconstructed,err:=PlanNativeGatewayInstances(top,devices,pair.Postgres.ImageAlias,pair.LiteLLM.ImageAlias)
	if err!=nil || !sameGatewayPair(pair,reconstructed) {return fail("intent",fmt.Errorf("gateway pair not derived from validated topology"))}
	inspect:=func()error {
		if err:=ObserveNativeGatewayAttachmentsWithReader(ctx,top,devices,client);err!=nil{return err}
		obs,err:=ObserveNativeGatewayInstancesWithReader(ctx,pair,client)
		if err!=nil{return err}
		if len(obs)!=2 || obs[0].Status!=NativeGatewayCompatible || obs[1].Status!=NativeGatewayCompatible {
			return fmt.Errorf("native gateway requires both existing owned service instances")
		}
		return ctx.Err()
	}
	if err:=inspect();err!=nil{return fail("preflight",err)}
	scope,_:=incusscope.NewProjectScope(top.Project)
	for _,spec:=range []NativeGatewayInstanceSpec{pair.LiteLLM,pair.Postgres} {
		if err:=ctx.Err();err!=nil{return fail(spec.Name,err)}
		instance,_,err:=client.GetInstance(spec.Name)
		if err!=nil || instance==nil || instance.Project!=top.Project {
			return fail(spec.Name,fmt.Errorf("current service state unavailable"))
		}
		if instance.Status=="Stopped" {
			out.Verified=append(out.Verified,spec.Name)
			continue
		}
		if instance.Status!="Running" {return fail(spec.Name,fmt.Errorf("service lifecycle status unverified"))}
		if err:=guard(ctx);err!=nil{return fail("authority",err)}
		if err:=inspect();err!=nil{return fail("mutation-boundary",err)}
		current,_,err:=client.GetInstance(spec.Name)
		if err!=nil || current==nil || current.Status!="Running" {
			return fail("mutation-boundary",fmt.Errorf("service state changed before stop"))
		}
		stopped,err:=native.EnsureInstanceStateWithClient(ctx,native.InstanceLifecycleIntent{
			Scope:scope,Name:spec.Name,Resource:"gateway-instance",Role:spec.Role,EnvironmentID:spec.EnvironmentID,State:native.InstanceStopped,
		},client)
		if err!=nil {return fail(spec.Name,err)}
		if !stopped.Verified {return fail(spec.Name,fmt.Errorf("stop completion unverified"))}
		if stopped.Changed {out.Changed=append(out.Changed,spec.Name)}
		// Protect the retained database attachment and both instance identities
		// after each transition; never claim verification on a drifted resource.
		if err:=inspect();err!=nil{return fail("postcondition",err)}
		out.Verified=append(out.Verified,spec.Name)
	}
	return out,ctx.Err()
}
