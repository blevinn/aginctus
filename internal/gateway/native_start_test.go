package gateway

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration/native"
	"github.com/lxc/incus/v7/shared/api"
)
type gatewayStartFake struct {
	*fakeGatewayStoppedClient
	transitions []string
}
func(f *gatewayStartFake)UpdateInstanceState(name string,req api.InstanceStatePut,etag string)(native.InstanceOperation,error){
	if req.Action!="start" || req.Force {return nil,fmt.Errorf("unexpected operation")}
	f.transitions=append(f.transitions,name)
	f.instances[name].Status="Running"
	return successfulGatewayCreateOperation{},nil
}
type gatewayReadyFunc func(context.Context,string,string)error
func(f gatewayReadyFunc)VerifyGatewayServiceReady(ctx context.Context,p,r string)error{return f(ctx,p,r)}
func startFixture(t *testing.T)(NativeGatewayTopology,NativeGatewayDevices,NativeGatewayInstancePair,*gatewayStartFake,NativeGatewayRuntimeBinding){
	t.Helper()
	top,devices,pair,client:=stoppedClientFixture(t)
	_,err:=CreateStoppedGatewayWithClient(context.Background(),top,devices,pair,client,func(context.Context)error{return nil})
	if err!=nil {t.Fatal(err)}
	b:=NativeGatewayRuntimeBinding{
		Project:top.Project,AcceptedRevision:strings.Repeat("a",64),CredentialVersion:"v1",
		PostgresInstance:"postgres-1",LiteLLMInstance:"litellm-1",
	}
	return top,devices,pair,&gatewayStartFake{fakeGatewayStoppedClient:client},b
}
func TestNativeGatewayStartRequiresPostgresReadiness(t *testing.T) {
	top,devices,pair,client,b:=startFixture(t)
	guards:=0
	evidence:=runtimeVerifierFunc(func(context.Context,NativeGatewayRuntimeBinding)error{return nil})
	probes:=[]string{}
	probe:=gatewayReadyFunc(func(_ context.Context,p,r string)error{
		if p!=top.Project {t.Fatal("wrong project")}
		probes=append(probes,r)
		return nil
	})
	res,err:=StartNativeGatewayWithClient(context.Background(),top,devices,pair,client,
		func(context.Context)error{guards++;return nil},b,evidence,probe)
	if err!=nil || guards!=2 || len(res.Ready)!=2 || len(res.Started)!=2 ||
		client.transitions[0]!="postgres-1" || client.transitions[1]!="litellm-1" ||
		probes[0]!="postgres" || probes[1]!="litellm" {t.Fatalf("ordered startup: %+v %v transitions=%v probes=%v",res,err,client.transitions,probes)}
	res,err=StartNativeGatewayWithClient(context.Background(),top,devices,pair,client,
		func(context.Context)error{return nil},b,evidence,probe)
	if err!=nil || len(res.Started)!=0 || len(res.Ready)!=2 || len(client.transitions)!=2 {t.Fatalf("repeat startup: %+v %v",res,err)}
}
func TestNativeGatewayStartRefusesMissingAuthorityOrDatabaseReadiness(t *testing.T) {
	top,devices,pair,client,b:=startFixture(t)
	evidence:=runtimeVerifierFunc(func(context.Context,NativeGatewayRuntimeBinding)error{return nil})
	probe:=gatewayReadyFunc(func(_ context.Context,_,role string)error{
		if role=="postgres" {return fmt.Errorf("synthetic database unready")}
		return nil
	})
	if _,err:=StartNativeGatewayWithClient(context.Background(),top,devices,pair,client,nil,b,evidence,probe);err==nil{
		t.Fatal("nil writer guard accepted")
	}
	if len(client.transitions)!=0 {t.Fatal("wrote without authority")}
	res,err:=StartNativeGatewayWithClient(context.Background(),top,devices,pair,client,
		func(context.Context)error{return nil},b,evidence,probe)
	if err==nil || len(res.Started)!=1 || len(res.Ready)!=0 || len(client.transitions)!=1 ||
		client.transitions[0]!="postgres-1" {t.Fatalf("startup progressed past failed database readiness: %+v %v",res,err)}
}
func TestNativeGatewayStartStopsAfterCredentialRevocation(t *testing.T) {
	top,devices,pair,client,b:=startFixture(t)
	calls:=0
	evidence:=runtimeVerifierFunc(func(context.Context,NativeGatewayRuntimeBinding)error{
		calls++
		if calls>=3{return fmt.Errorf("revoked")}
		return nil
	})
	ready:=gatewayReadyFunc(func(context.Context,string,string)error{return nil})
	res,err:=StartNativeGatewayWithClient(context.Background(),top,devices,pair,client,
		func(context.Context)error{return nil},b,evidence,ready)
	if err==nil || len(res.Started)!=1 || len(res.Ready)!=1 ||
		len(client.transitions)!=1 || client.transitions[0]!="postgres-1" {
		t.Fatalf("credential revocation did not fence second service: %+v %v",res,err)
	}
}

func TestNativeGatewayStartRejectsRevokedCredentialBeforeWrites(t *testing.T) {
	top,devices,pair,client,b:=startFixture(t)
	secret:="synthetic-private-credential"
	deny:=runtimeVerifierFunc(func(context.Context,NativeGatewayRuntimeBinding)error{return fmt.Errorf(secret)})
	ready:=gatewayReadyFunc(func(context.Context,string,string)error{t.Fatal("readiness called after failed delivery");return nil})
	res,err:=StartNativeGatewayWithClient(context.Background(),top,devices,pair,client,
		func(context.Context)error{return nil},b,deny,ready)
	if err==nil || len(res.Started)!=0 || len(client.transitions)!=0 ||
		strings.Contains(err.Error(),secret) {t.Fatalf("revoked evidence not fenced or redacted: %+v %v",res,err)}
}
func TestNativeGatewayStartFencesSecondServiceAfterGuardLoss(t *testing.T) {
	top,devices,pair,client,b:=startFixture(t)
	verify:=runtimeVerifierFunc(func(context.Context,NativeGatewayRuntimeBinding)error{return nil})
	ready:=gatewayReadyFunc(func(context.Context,string,string)error{return nil})
	calls:=0
	guard:=func(context.Context)error{
		calls++
		if calls==2{return fmt.Errorf("writer lease revoked")}
		return nil
	}
	res,err:=StartNativeGatewayWithClient(context.Background(),top,devices,pair,client,guard,b,verify,ready)
	if err==nil || len(res.Started)!=1 || len(res.Ready)!=1 ||
		len(client.transitions)!=1 || client.transitions[0]!="postgres-1" {
		t.Fatalf("second service not fenced after guard loss: %+v %v",res,err)
	}
}
