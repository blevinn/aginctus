package gateway

import (
	"context"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

// NativeEnvironmentGatewayStartClient exposes only observation and lifecycle
// state transitions; no create/update/delete for images, instances or volumes.
type NativeEnvironmentGatewayStartClient interface { NativeGatewayStopClient }

// NativeEnvironmentGatewayReadiness is a separately trusted service probe.
// A successful Incus Running observation is never a readiness result. The
// implementation must authenticate each service, avoid secret-bearing errors,
// and bind its guest target to the exact environment resource identities.
type NativeEnvironmentGatewayReadiness interface {
	VerifyGatewayServiceReady(context.Context,NativeGatewayResourceIdentity,string)error
}

type NativeEnvironmentGatewayStartResult struct {
	Project string `json:"project"`
	EnvironmentID string `json:"environmentId"`
	Started []string `json:"started,omitempty"`
	Ready []string `json:"ready,omitempty"`
}

type NativeEnvironmentGatewayStartError struct {
	Stage string
	Service string
	Attempted bool
	Uncertain bool
}
func (e *NativeEnvironmentGatewayStartError) Error()string{
	return "environment gateway start failed at "+e.Stage+" for "+e.Service+"; inspect selected environment and retained PostgreSQL volume before retry"
}

// StartNativeEnvironmentGatewayWithClient is a library-only, unactivated
// implementation. The caller must enforce a live writer fence in guard,
// a project-scoped independently accepted environment revision and verified
// guest credential delivery in verifier, and authenticated readiness in ready.
// None may self-assert authority. No production caller is provided here.
//
// PostgreSQL must be running AND authenticated before LiteLLM may start.
// Every stage rechecks the same immutable environment/Incus identity and
// the current accepted-revision/credential version; failures preserve partial
// results without blind retry, rollback, or deletion of persistent storage.
func StartNativeEnvironmentGatewayWithClient(
	ctx context.Context, descriptor environment.Descriptor, selected environment.Binding,
	top NativeGatewayTopology, devices NativeGatewayDevices, pair NativeGatewayInstancePair,
	binding NativeGatewayRuntimeBinding, client NativeEnvironmentGatewayStartClient,
	guard func(context.Context)error, verifier NativeGatewayDeliveryVerifier,
	ready NativeEnvironmentGatewayReadiness,
)(NativeEnvironmentGatewayStartResult,error){
	result:=NativeEnvironmentGatewayStartResult{Project:top.Project,EnvironmentID:top.EnvironmentID}
	fail:=func(stage,service string,attempted bool)(NativeEnvironmentGatewayStartResult,error){
		return result,&NativeEnvironmentGatewayStartError{
			Stage:stage,Service:service,Attempted:attempted,Uncertain:attempted,
		}
	}
	if guard==nil || verifier==nil || ready==nil{return fail("authority","gateway",false)}
	identity,err:=NativeGatewayIdentity(descriptor,selected)
	if err!=nil || top.Project!=identity.Project || top.EnvironmentID!=identity.EnvironmentID ||
		top.DatabaseVolume!=identity.DatabaseVolume {
		return fail("environment","gateway",false)
	}
	expected,err:=PlanNativeGatewayInstances(top,devices,pair.Postgres.ImageAlias,pair.LiteLLM.ImageAlias)
	if err!=nil || !sameGatewayPair(pair,expected) || binding.Validate(descriptor,selected,pair)!=nil{
		return fail("intent","gateway",false)
	}
	scope,_:=incusscope.NewProjectScope(identity.Project)
	check:=func()bool {
		if err:=ctx.Err();err!=nil{return false}
		if err:=ObserveNativeGatewayAttachmentsWithReader(ctx,top,devices,client);err!=nil{return false}
		obs,err:=ObserveNativeGatewayInstancesWithReader(ctx,pair,client)
		return err==nil && len(obs)==2 &&
			obs[0].Status==NativeGatewayCompatible && obs[1].Status==NativeGatewayCompatible
	}
	if !check(){return fail("preflight","gateway",false)}
	for _,spec:=range []NativeGatewayInstanceSpec{pair.Postgres,pair.LiteLLM}{
		if err:=guard(ctx);err!=nil{return fail("authority",spec.Name,false)}
		if err:=VerifyNativeGatewayRuntimeBinding(ctx,descriptor,selected,pair,binding,verifier);err!=nil{
			return fail("credentials",spec.Name,false)
		}
		if !check(){return fail("mutation-boundary",spec.Name,false)}
		instance,_,err:=client.GetInstance(spec.Name)
		if err!=nil || instance==nil || instance.Project!=identity.Project ||
			!gatewayOwnerMatches(instance.Config,identity.EnvironmentID) ||
			(instance.Status!="Stopped" && instance.Status!="Running") {
			return fail("state",spec.Name,false)
		}
		attempted:=false
		if instance.Status=="Stopped"{
			attempted=true
			transition,err:=native.EnsureInstanceStateWithClient(ctx,native.InstanceLifecycleIntent{
				Scope:scope,Name:spec.Name,Resource:"gateway-instance",Role:spec.Role,State:native.InstanceRunning,
			},client)
			if err!=nil || !transition.Verified{return fail("transition",spec.Name,true)}
			if transition.Changed {result.Started=append(result.Started,spec.Name)}
		}
		if !check(){return fail("postcondition",spec.Name,attempted)}
		if err:=VerifyNativeGatewayRuntimeBinding(ctx,descriptor,selected,pair,binding,verifier);err!=nil{
			return fail("credentials-post-start",spec.Name,attempted)
		}
		if err:=ready.VerifyGatewayServiceReady(ctx,identity,spec.Role);err!=nil{
			return fail("readiness",spec.Name,attempted)
		}
		if err:=ctx.Err();err!=nil{return fail("cancellation",spec.Name,attempted)}
		result.Ready=append(result.Ready,spec.Name)
	}
	return result,nil
}
