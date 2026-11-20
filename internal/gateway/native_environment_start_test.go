package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/lxc/incus/v7/shared/api"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

type environmentStartClient struct{
	*fakeGatewayStoppedClient
	transitions []string
}
func(c *environmentStartClient)UpdateInstanceState(name string,req api.InstanceStatePut,_ string)(native.InstanceOperation,error){
	if req.Action!="start" || req.Force{return nil,errors.New("unexpected gateway transition")}
	c.transitions=append(c.transitions,name)
	c.instances[name].Status="Running"
	return successfulGatewayCreateOperation{},nil
}
type envGatewayReadyFunc func(context.Context,NativeGatewayResourceIdentity,string)error
func(f envGatewayReadyFunc)VerifyGatewayServiceReady(ctx context.Context,id NativeGatewayResourceIdentity,role string)error{return f(ctx,id,role)}
func environmentStartFixture(t *testing.T)(NativeGatewayTopology,NativeGatewayDevices,NativeGatewayInstancePair,*environmentStartClient,NativeGatewayRuntimeBinding){
	t.Helper()
	top,devices,pair,client:=environmentGatewayCreateFixture(t)
	if _,err:=CreateStoppedGatewayWithClient(context.Background(),top,devices,pair,client,func(context.Context)error{return nil});err!=nil{t.Fatal(err)}
	desc:=gatewayTestEnvironment(top.EnvironmentID,top.Project)
	id,err:=NativeGatewayIdentity(desc,desc.Binding)
	if err!=nil{t.Fatal(err)}
	b:=NativeGatewayRuntimeBinding{Resources:id,AcceptedEnvironmentRevision:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",CredentialVersion:"v1"}
	return top,devices,pair,&environmentStartClient{fakeGatewayStoppedClient:client},b
}
func TestEnvironmentGatewayStartOrderedAndIdempotent(t *testing.T){
	top,devices,pair,client,b:=environmentStartFixture(t)
	desc:=gatewayTestEnvironment(top.EnvironmentID,top.Project)
	roles:=[]string{}
	probe:=envGatewayReadyFunc(func(_ context.Context,id NativeGatewayResourceIdentity,role string)error{
		if id!=b.Resources {t.Fatal("readiness target mismatch")}
		roles=append(roles,role)
		return nil
	})
	verify:=gatewayDeliveryFunc(func(context.Context,NativeGatewayRuntimeBinding)error{return nil})
	result,err:=StartNativeEnvironmentGatewayWithClient(context.Background(),desc,desc.Binding,
		top,devices,pair,b,client,func(context.Context)error{return nil},verify,probe)
	if err!=nil || len(result.Started)!=2 || len(result.Ready)!=2 ||
		result.Started[0]!=pair.Postgres.Name || result.Started[1]!=pair.LiteLLM.Name ||
		len(roles)!=2 || roles[0]!="postgres" || roles[1]!="litellm"{
		t.Fatalf("gateway startup order: %+v %v roles=%v",result,err,roles)
	}
	repeat,err:=StartNativeEnvironmentGatewayWithClient(context.Background(),desc,desc.Binding,
		top,devices,pair,b,client,func(context.Context)error{return nil},verify,probe)
	if err!=nil || len(repeat.Started)!=0 || len(repeat.Ready)!=2 || len(client.transitions)!=2{
		t.Fatalf("idempotent gateway start: %+v %v",repeat,err)
	}
}
func TestEnvironmentGatewayStartStopsAtUnreadyPostgres(t *testing.T){
	top,devices,pair,client,b:=environmentStartFixture(t)
	desc:=gatewayTestEnvironment(top.EnvironmentID,top.Project)
	verify:=gatewayDeliveryFunc(func(context.Context,NativeGatewayRuntimeBinding)error{return nil})
	probe:=envGatewayReadyFunc(func(_ context.Context,_ NativeGatewayResourceIdentity,role string)error{
		if role!="postgres"{t.Fatal("LiteLLM readiness must not run")}
		return errors.New("synthetic-not-ready")
	})
	result,err:=StartNativeEnvironmentGatewayWithClient(context.Background(),desc,desc.Binding,
		top,devices,pair,b,client,func(context.Context)error{return nil},verify,probe)
	if err==nil || len(result.Started)!=1 || len(result.Ready)!=0 ||
		client.instances[pair.LiteLLM.Name].Status!="Stopped"{
		t.Fatalf("unready PostgreSQL did not stop progression: %+v %v",result,err)
	}
}
func TestEnvironmentGatewayStartRejectsForeignOwnerOrMissingAuthority(t *testing.T){
	top,devices,pair,client,b:=environmentStartFixture(t)
	desc:=gatewayTestEnvironment(top.EnvironmentID,top.Project)
	verify:=gatewayDeliveryFunc(func(context.Context,NativeGatewayRuntimeBinding)error{return nil})
	probe:=envGatewayReadyFunc(func(context.Context,NativeGatewayResourceIdentity,string)error{return nil})
	if _,err:=StartNativeEnvironmentGatewayWithClient(context.Background(),desc,desc.Binding,
		top,devices,pair,b,client,nil,verify,probe);err==nil || len(client.transitions)!=0{
		t.Fatal("nil lease accepted")
	}
	client.instances[pair.Postgres.Name].Config["user.aginctus.environment-id"]="123e4567-e89b-42d3-a456-426614174001"
	if _,err:=StartNativeEnvironmentGatewayWithClient(context.Background(),desc,desc.Binding,
		top,devices,pair,b,client,func(context.Context)error{return nil},verify,probe);err==nil ||
		len(client.transitions)!=0 {
		t.Fatal("foreign environment guest started")
	}
}
