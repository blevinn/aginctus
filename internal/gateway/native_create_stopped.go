package gateway

import (
	"context"
	"fmt"
	"maps"
	"slices"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	"github.com/blevinn/aginctus/internal/orchestration/native"
)

// NativeGatewayStoppedCreateClient is the minimum SDK surface required to
// create, but never start, update or delete, gateway service containers.
type NativeGatewayStoppedCreateClient interface {
	NativeGatewayInstanceReader
	NativeGatewayAttachmentReader
	CreateInstance(api.InstancesPost) (native.InstanceCreateOperation, error)
}

type nativeGatewayStoppedSDK struct { server incusclient.InstanceServer }
func (c nativeGatewayStoppedSDK) GetConnectionInfo()(*incusclient.ConnectionInfo,error){return c.server.GetConnectionInfo()}
func (c nativeGatewayStoppedSDK) GetProject(s string)(*api.Project,string,error){return c.server.GetProject(s)}
func (c nativeGatewayStoppedSDK) GetInstance(s string)(*api.Instance,string,error){return c.server.GetInstance(s)}
func (c nativeGatewayStoppedSDK) GetImageAlias(s string)(*api.ImageAliasesEntry,string,error){return c.server.GetImageAlias(s)}
func (c nativeGatewayStoppedSDK) GetImage(s string)(*api.Image,string,error){return c.server.GetImage(s)}
func (c nativeGatewayStoppedSDK) GetNetwork(s string)(*api.Network,string,error){return c.server.GetNetwork(s)}
func (c nativeGatewayStoppedSDK) GetStoragePool(s string)(*api.StoragePool,string,error){return c.server.GetStoragePool(s)}
func (c nativeGatewayStoppedSDK) GetStoragePoolVolume(p,k,n string)(*api.StorageVolume,string,error){return c.server.GetStoragePoolVolume(p,k,n)}
func (c nativeGatewayStoppedSDK) CreateInstance(p api.InstancesPost)(native.InstanceCreateOperation,error){return c.server.CreateInstance(p)}

// NativeGatewayStoppedResult preserves completed, verified service transitions
// when a subsequent operation fails. A submitted create may have succeeded even
// if its response or verification fails. No automatic rollback is attempted.
type NativeGatewayStoppedResult struct {
	Project string `json:"project"`
	Plan NativeGatewayCreatePlan `json:"plan"`
	Created []string `json:"created"`
	Verified []string `json:"verified"`
}

type NativeGatewayStoppedError struct {
	Stage string
	Service string
	Attempted bool
	Uncertain bool
	cause error
}
func (e *NativeGatewayStoppedError) Error() string {
	return "native gateway stopped creation failed at "+e.Stage+" for "+e.Service+"; inspect exact project resources before retry; no automatic cleanup"
}
func (e *NativeGatewayStoppedError) Unwrap()error{return e.cause}

// CreateStoppedGatewayWithClient intentionally has no local-connection
// wrapper or CLI activation. The caller must separately enforce accepted-state
// authority and exclusive writer fencing for both project and host resources.
// guard is rechecked before each write; a nil guard forbids execution.
//
// No credentials, OCI commands or runtime environment are configured here.
// Both services remain STOPPED; a later stage must prove configuration and
// credentials before any service can be started.
func CreateStoppedGatewayWithClient(ctx context.Context, top NativeGatewayTopology, devices NativeGatewayDevices, pair NativeGatewayInstancePair, client NativeGatewayStoppedCreateClient, guard func(context.Context)error) (NativeGatewayStoppedResult,error) {
	result:=NativeGatewayStoppedResult{Project:top.Project}
	fail:=func(stage,service string,attempted bool,cause error)(NativeGatewayStoppedResult,error){
		return result,&NativeGatewayStoppedError{Stage:stage,Service:service,Attempted:attempted,Uncertain:attempted,cause:cause}
	}
	if guard==nil {return fail("authority","gateway",false,fmt.Errorf("native gateway creation requires an authority guard"))}
	// All candidate data is treated as untrusted, even for the dry-run path.
	original,err:=PlanNativeGatewayInstances(top,devices,pair.Postgres.ImageAlias,pair.LiteLLM.ImageAlias)
	if err!=nil || !sameGatewayPair(pair,original) {
		return fail("intent","gateway",false,fmt.Errorf("native gateway pair not derived from validated topology"))
	}
	if err:=ctx.Err();err!=nil{return fail("cancellation","gateway",false,err)}
	if err:=ObserveNativeGatewayAttachmentsWithReader(ctx,top,devices,client);err!=nil{
		return fail("attachments","gateway",false,err)
	}
	observed,err:=ObserveNativeGatewayInstancesWithReader(ctx,pair,client)
	if err!=nil {return fail("instances","gateway",false,err)}
	plan,err:=PlanNativeGatewayCreate(pair,observed)
	if err!=nil {return fail("plan","gateway",false,err)}
	result.Plan=plan
	for index,spec:=range []NativeGatewayInstanceSpec{pair.Postgres,pair.LiteLLM} {
		step:=plan.Steps[index]
		if step.Action==NativeGatewayRetainExisting {
			result.Verified=append(result.Verified,spec.Name)
			continue
		}
		if err:=guard(ctx);err!=nil{return fail("authority",spec.Name,false,err)}
		if err:=ObserveNativeGatewayAttachmentsWithReader(ctx,top,devices,client);err!=nil {
			return fail("mutation-boundary-attachments",spec.Name,false,err)
		}
		current,err:=ObserveNativeGatewayInstancesWithReader(ctx,pair,client)
		if err!=nil{return fail("mutation-boundary-instances",spec.Name,false,err)}
		replanned,err:=PlanNativeGatewayCreate(pair,current)
		if err!=nil || replanned.Steps[index].Action!=NativeGatewayCreateStopped {
			return fail("mutation-boundary",spec.Name,false,fmt.Errorf("gateway instance absence not stable"))
		}
		for j:=index+1;j<len(plan.Steps);j++ {
			if replanned.Steps[j].Action!=plan.Steps[j].Action {
				return fail("mutation-boundary",spec.Name,false,fmt.Errorf("gateway sibling changed before creation"))
			}
		}
		if err:=ctx.Err();err!=nil{return fail("cancellation",spec.Name,false,err)}
		alias,_,err:=client.GetImageAlias(spec.ImageAlias)
		if err!=nil || alias==nil || alias.Name!=spec.ImageAlias {
			return fail("image",spec.Name,false,fmt.Errorf("selected image alias unavailable"))
		}
		// A service-specific marker keeps this resource distinct from
		// workload and historical Compose instances.
		request:=api.InstancesPost{
			Name:spec.Name,Type:api.InstanceTypeContainer,
			Source:api.InstanceSource{Type:"image",Fingerprint:alias.Target},
			InstancePut:api.InstancePut{
				Description:"Aginctus native gateway "+spec.Role,
				Profiles:[]string{},
				Config:map[string]string{
					"user.aginctus.managed":"true",
					"user.aginctus.resource":"gateway-instance",
					"user.aginctus.role":spec.Role,
					"user.aginctus.deployment":spec.Project,
				},
				Devices:cloneGatewayDevices(spec.Devices),
			},
		}
		op,err:=client.CreateInstance(request)
		if err!=nil{return fail("create",spec.Name,true,err)}
		result.Created=append(result.Created,spec.Name)
		if op==nil{return fail("operation",spec.Name,true,fmt.Errorf("instance operation missing"))}
		if err:=op.WaitContext(ctx);err!=nil{return fail("operation",spec.Name,true,err)}
		inst,_,err:=client.GetInstance(spec.Name)
		if err!=nil || inst==nil || inst.Name!=spec.Name ||
			inst.Project!=spec.Project || string(inst.Type)!="container" ||
			inst.Status!="Stopped" || inst.Config["volatile.base_image"]!=alias.Target ||
			inst.Config["user.aginctus.managed"]!="true" ||
			inst.Config["user.aginctus.resource"]!="gateway-instance" ||
			inst.Config["user.aginctus.role"]!=spec.Role ||
			inst.Config["user.aginctus.deployment"]!=spec.Project ||
			len(inst.Profiles)!=0 || !equalNativeDevices(inst.Devices,spec.Devices) {
			return fail("verification",spec.Name,true,fmt.Errorf("created gateway service postcondition unverified"))
		}
		if err:=ctx.Err();err!=nil{return fail("verification",spec.Name,true,err)}
		result.Verified=append(result.Verified,spec.Name)
	}
	return result,nil
}

func cloneGatewayDevices(source map[string]map[string]string)map[string]map[string]string{
	output:=make(map[string]map[string]string,len(source))
	for k,v:=range source{output[k]=maps.Clone(v)}
	return output
}
func sameGatewayPair(a,b NativeGatewayInstancePair)bool{
	eq:=func(x,y NativeGatewayInstanceSpec)bool{
		return x.Project==y.Project && x.Name==y.Name && x.Role==y.Role &&
			x.ImageAlias==y.ImageAlias && slices.Equal(x.DependsOn,y.DependsOn) &&
			slices.Equal(x.SecretEnvironment,y.SecretEnvironment) &&
			equalNativeDevices(x.Devices,y.Devices)
	}
	return eq(a.Postgres,b.Postgres)&&eq(a.LiteLLM,b.LiteLLM)
}
