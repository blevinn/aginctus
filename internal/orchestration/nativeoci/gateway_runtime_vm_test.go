package nativeoci_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/nativeoci"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

// Reached only by TestDaemonOCI after the disposable marker, fresh local-only
// daemon, and real image acquisition. This deliberately exercises the actual
// domain publisher, launch configuration, ordered start and readiness probes,
// not the installed mTLS/admin coordinator. All credentials are synthetic.
func testNativeAuthenticatedPair(t *testing.T,ctx context.Context,admin,client incus.InstanceServer,desc environment.Descriptor,acquired map[string]gateway.NativeGatewayCreateImage,resolved map[string]nativeoci.ResolvedImage){
	t.Helper()
	desc.ID="123e4567-e89b-42d3-a456-426614174099"
	const bridge="gw-runtime-mgmt"
	if err:=admin.UseProject("default").CreateNetwork(api.NetworksPost{Name:bridge,Type:"bridge",NetworkPut:api.NetworkPut{Config:map[string]string{
		"ipv4.address":"10.93.0.1/24","ipv4.nat":"false","ipv4.routing":"false","ipv4.dhcp.gateway":"none",
		"ipv6.address":"none","dns.mode":"managed","user.aginctus.managed":"true","user.aginctus.resource":"management-network",
	}}});err!=nil{t.Fatal("create private disposable gateway bridge failed")}
	guard:=func(ctx context.Context)error{return ctx.Err()}
	if volume,err:=gateway.EnsureNativeEnvironmentGatewayVolumeWithClient(ctx,desc,desc.Binding,"spike",client,guard);err!=nil||!volume.Created||!volume.Verified{t.Fatal("fresh runtime database volume unavailable")}
	ref:=func(role string)string{i:=resolved[role];return i.Registry+"/"+i.Repository+"@"+i.ManifestDigest}
	spec:=gateway.Spec{ID:desc.ID,Project:desc.Binding.Project,Network:bridge,PostgresImage:ref("postgres"),LiteLLMImage:ref("litellm")}
	top,err:=spec.NativeTopology()
	if err!=nil{t.Fatal(err)}
	top,err=gateway.BindNativeGatewayTopology(top,desc,desc.Binding)
	if err!=nil{t.Fatal(err)}
	devices,err:=gateway.PlanNativeGatewayDevices(top,"spike","spike")
	if err!=nil{t.Fatal(err)}
	pair,err:=gateway.PlanNativeGatewayInstances(top,devices,acquired["postgres"].Evidence.Alias,acquired["litellm"].Evidence.Alias)
	if err!=nil{t.Fatal(err)}
	images:=gateway.NativeGatewayCreateImages{Postgres:acquired["postgres"],LiteLLM:acquired["litellm"]}
	created,err:=gateway.CreateStoppedGatewayWithClient(ctx,top,devices,pair,images,gatewayStoppedVMClient{client},guard)
	if err!=nil||len(created.Created)!=2{t.Fatal("fresh real-image stopped pair creation failed")}
	token:=func()string{var b [32]byte;if _,err:=rand.Read(b[:]);err!=nil{t.Fatal("synthetic token generation failed")};return hex.EncodeToString(b[:])}
	password,master,salt:=token(),token(),token()
	material,err:=gateway.NewGatewayCredentialMaterial("runtime-v1",password,master,salt)
	if err!=nil{t.Fatal("synthetic material rejected")}
	imageSpec:=func(role string)environment.GatewayImageSpec{i:=resolved[role];return environment.GatewayImageSpec{Registry:i.Registry,Repository:i.Repository,Architecture:runtime.GOARCH,ManifestDigest:i.ManifestDigest,ConfigDigest:i.ConfigDigest,IncusFingerprint:i.IncusFingerprint,Alias:acquired[role].Evidence.Alias}}
	candidate:=environment.Candidate{APIVersion:environment.CandidateAPIVersion,Kind:environment.CandidateKind,Environment:desc,SelectedWorkloads:[]string{},Workloads:[]acceptedstate.WorkloadSpec{},Gateway:&environment.GatewaySpec{
		ManagementNetwork:bridge,RootPool:"spike",DatabasePool:"spike",Postgres:imageSpec("postgres"),LiteLLM:imageSpec("litellm"),CredentialVersion:"runtime-v1",EnsureRunning:true,
	}}
	revision,err:=environment.BuildAcceptedRevision(nil,nil,candidate)
	if err!=nil{t.Fatal("runtime accepted pin construction failed")}
	receipt,err:=gateway.StageStoppedGatewayCredentialFiles(ctx,revision,candidate,top,devices,pair,images,resolved["postgres"],resolved["litellm"],material,client,guard)
	if err!=nil||!receipt.Verified{t.Fatalf("runtime credential stage failed: %v",err)}
	for _,s:=range []gateway.NativeGatewayInstanceSpec{pair.Postgres,pair.LiteLLM}{
		i,etag,err:=client.GetInstance(s.Name)
		if err!=nil||i==nil||i.Status!="Stopped"||etag==""{t.Fatal("runtime launcher target unverified")}
		put:=i.Writable()
		put.Config=maps.Clone(put.Config)
		maps.Copy(put.Config,gateway.NativeGatewayLauncherConfig(s,pair.Postgres.Name))
		op,err:=client.UpdateInstance(s.Name,put,etag)
		if err!=nil||op==nil||op.WaitContext(ctx)!=nil{t.Fatal("runtime launcher configuration failed")}
	}
	identity,err:=gateway.NativeGatewayIdentity(desc,desc.Binding)
	if err!=nil{t.Fatal(err)}
	binding:=gateway.NativeGatewayRuntimeBinding{Resources:identity,AcceptedEnvironmentRevision:revision,CredentialVersion:"runtime-v1",Images:images,RootPool:"spike",DatabasePool:"spike"}
	verify:=runtimeDeliveryCheck(func(ctx context.Context,b gateway.NativeGatewayRuntimeBinding)error{
		if b!=binding{return errors.New("fixture runtime binding changed")}
		for _,s:=range []gateway.NativeGatewayInstanceSpec{pair.Postgres,pair.LiteLLM}{
			i,_,err:=client.GetInstance(s.Name)
			if err!=nil||i==nil{return errors.New("fixture guest unavailable")}
			at:=receipt.PostgresCreatedAt;if s.Role=="litellm"{at=receipt.LiteLLMCreatedAt}
			if !i.CreatedAt.Equal(at){return errors.New("fixture guest replaced")}
			for k,v:=range gateway.NativeGatewayLauncherConfig(s,pair.Postgres.Name){if i.Config[k]!=v||i.ExpandedConfig[k]!=v{return errors.New("fixture launcher changed")}}
			raw,err:=json.Marshal(i)
			if err!=nil||strings.Contains(string(raw),password)||strings.Contains(string(raw),master)||strings.Contains(string(raw),salt){return errors.New("fixture instance metadata exposed a credential")}
		}
		return ctx.Err()
	})
	probe:=runtimeProbeSDK{client}
	ready:=runtimeReadyCheck(func(ctx context.Context,id gateway.NativeGatewayResourceIdentity,role string)error{
		if id!=identity{return errors.New("fixture probe scope changed")}
		probeCtx,cancel:=context.WithTimeout(ctx,2*time.Minute);defer cancel()
		for {
			var err error
			switch role{
			case "postgres":err=gateway.VerifyNativeGatewayPostgresReadyWithClient(probeCtx,desc,desc.Binding,probe)
			case "litellm":err=gateway.VerifyNativeGatewayLiteLLMReadyWithClient(probeCtx,desc,desc.Binding,probe)
			default:return errors.New("unknown fixture role")
			}
			if err==nil{return nil}
			select{case <-probeCtx.Done():return errors.New("authenticated fixture probe timed out");case <-time.After(time.Second):}
		}
	})
	start:=func(wantChanges int){
		t.Helper()
		got,err:=gateway.StartNativeEnvironmentGatewayWithClient(ctx,desc,desc.Binding,top,devices,pair,binding,gatewayStopVMClient{client},guard,verify,ready)
		if err!=nil||len(got.Started)!=wantChanges||len(got.Ready)!=2||got.Ready[0]!=pair.Postgres.Name||got.Ready[1]!=pair.LiteLLM.Name{t.Fatalf("authenticated native start: %v",err)}
	}
	stop:=func(){
		t.Helper()
		got,err:=gateway.StopNativeGatewayWithClient(ctx,top,devices,pair,gatewayStopVMClient{client},guard)
		if err!=nil||len(got.Changed)!=2||got.Changed[0]!=pair.LiteLLM.Name||got.Changed[1]!=pair.Postgres.Name{t.Fatalf("native graceful stop: %v",err)}
	}
	start(2)
	start(0)
	if err:=execOCI(ctx,client,pair.Postgres.Name,postgresFixtureQuery("/run/aginctus/gateway/postgres.pgpass","CREATE TABLE aginctus_spike AS SELECT 42 AS value"));err!=nil{t.Fatal("runtime marker creation failed")}
	volume,_,err:=client.GetStoragePoolVolume("spike","custom",identity.DatabaseVolume)
	if err!=nil||volume==nil||volume.CreatedAt.IsZero(){t.Fatal("runtime retained volume unavailable")}
	stop()
	start(2)
	if err:=execOCI(ctx,client,pair.Postgres.Name,postgresFixtureQuery("/run/aginctus/gateway/postgres.pgpass",postgresFixtureMarkerQuery));err!=nil{t.Fatal("runtime retained data probe failed")}
	after,_,err:=client.GetStoragePoolVolume("spike","custom",identity.DatabaseVolume)
	if err!=nil||after==nil||!after.CreatedAt.Equal(volume.CreatedAt){t.Fatal("runtime database volume replaced")}
	stop()
	t.Log("actual OCI file consumers, authenticated ordered readiness, repeated startup, graceful stop and retained database marker verified")
}

type runtimeDeliveryCheck func(context.Context,gateway.NativeGatewayRuntimeBinding)error
func(f runtimeDeliveryCheck)VerifyGatewayDelivery(ctx context.Context,b gateway.NativeGatewayRuntimeBinding)error{return f(ctx,b)}
type runtimeReadyCheck func(context.Context,gateway.NativeGatewayResourceIdentity,string)error
func(f runtimeReadyCheck)VerifyGatewayServiceReady(ctx context.Context,id gateway.NativeGatewayResourceIdentity,role string)error{return f(ctx,id,role)}
type runtimeProbeSDK struct{incus.InstanceServer}
func(c runtimeProbeSDK)ExecGuest(name string,p api.InstanceExecPost,args *incus.InstanceExecArgs)(gateway.GatewayGuestExecOperation,error){return c.ExecInstance(name,p,args)}
