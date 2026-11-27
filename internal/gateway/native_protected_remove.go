package gateway

import (
	"context"
	"errors"
	"time"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	incus "github.com/lxc/incus/v7/client"
)

// RemoveLocalProtectedGatewayService discards one explicitly selected stopped
// root disk, never the retained database volume. The caller must independently
// quiesce use and approve loss of guest-local state. A prior delivery receipt
// remains evidence about the removed incarnation, not permission to recreate.
func RemoveLocalProtectedGatewayService(ctx context.Context, grantPath, operatorPath, role, uuid string, createdAt time.Time, discardRoot bool) (result NativeGatewayRemovalResult, id string, err error) {
	if !discardRoot || (role!="postgres" && role!="litellm") || !gatewayEnvironmentIDPattern.MatchString(uuid) || createdAt.IsZero(){
		return result,"",errProtectedGateway
	}
	e,err:=openProtectedExecution(ctx,grantPath,operatorPath,"gateway-remove-"+role)
	if err!=nil{return result,"",err}
	id=e.session.ID()
	defer func(){err=errors.Join(err,e.finish(ctx,err==nil))}()
	// No registry or credential source access is needed for removal.
	var images NativeGatewayCreateImages
	out:=[]*NativeGatewayCreateImage{&images.Postgres,&images.LiteLLM}
	for i,spec:=range []environment.GatewayImageSpec{e.candidate.Gateway.Postgres,e.candidate.Gateway.LiteLLM}{
		want:=native.GatewayOCIIdentity{Registry:spec.Registry,Repository:spec.Repository,ManifestDigest:spec.ManifestDigest,ConfigDigest:spec.ConfigDigest,IncusFingerprint:spec.IncusFingerprint}
		evidence,err:=e.provenance.ExistingImageEvidence(ctx,e.revision,gatewayRoles[i])
		if err!=nil||native.ObserveGatewayImageReuseWithClient(ctx,native.GatewayImageAcquireIntent{Project:e.grant.ProtectedProject,Image:want},spec.Alias,evidence,e.sdk)!=nil{return result,id,errProtectedGateway}
		*out[i]=NativeGatewayCreateImage{Expected:want,Evidence:evidence}
	}
	name:=e.plan.pair.LiteLLM.Name
	if role=="postgres"{name=e.plan.pair.Postgres.Name}
	intent:=NativeGatewayRemoval{Protected:&e.grant,Revision:e.revision,Instance:name,InstanceUUID:uuid,InstanceCreatedAt:createdAt}
	guard:=func(ctx context.Context,current NativeGatewayRemoval)error{
		if current!=intent || e.guard(ctx)!=nil{return errProtectedGateway}
		return ctx.Err()
	}
	sdk:=protectedRemoveSDK{InstanceServer:e.sdk,exec:e,ctx:ctx,intent:intent}
	physical:=environment.Binding{ServerID:e.grant.ServerID,Project:e.grant.ProtectedProject}
	result,err=RemoveNativeEnvironmentGatewayServiceWithClient(ctx,e.candidate.Environment,physical,e.plan.top,e.plan.devices,e.plan.pair,images,intent,sdk,guard)
	if err!=nil{return result,id,err}
	if !result.Absent || !result.DatabaseVolumeRetained || e.guard(ctx)!=nil || e.session.MarkApplied(ctx)!=nil{return result,id,errProtectedGateway}
	return result,id,nil
}

type protectedRemoveSDK struct{
	incus.InstanceServer
	exec *protectedExecution
	ctx context.Context
	intent NativeGatewayRemoval
}
func (s protectedRemoveSDK)DeleteInstance(name string)(native.InstanceOperation,error){
	if name!=s.intent.Instance||s.exec.guard(s.ctx)!=nil{return nil,errProtectedGateway}
	// Repeat the exact live incarnation checks after recording uncertainty.
	if s.exec.session.BeginEffect(s.ctx,"delete")!=nil||s.exec.guard(s.ctx)!=nil{return nil,errProtectedGateway}
	i,etag,err:=s.GetInstance(name)
	if err!=nil||i==nil||etag==""||i.Status!="Stopped"||i.Stateful||!i.CreatedAt.Equal(s.intent.InstanceCreatedAt)||i.Config["volatile.uuid"]!=s.intent.InstanceUUID||
		!gatewayRootDiscardableConfig(i){return nil,errProtectedGateway}
	op,err:=s.InstanceServer.DeleteInstance(name)
	if err!=nil||op==nil{return nil,errProtectedGateway}
	return protectedTrackedOperation{op,s.exec.session},nil
}
