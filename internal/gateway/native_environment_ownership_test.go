package gateway

import (
	"context"
	"testing"

	"github.com/lxc/incus/v7/shared/api"
)

func environmentGatewayCreateFixture(t *testing.T)(NativeGatewayTopology,NativeGatewayDevices,NativeGatewayInstancePair,*fakeGatewayStoppedClient){
	t.Helper()
	legacy,_,attachment:=nativeAttachmentAuditFixture(t)
	descriptor:=gatewayTestEnvironment("123e4567-e89b-42d3-a456-426614174000",legacy.Project)
	top,err:=BindNativeGatewayTopology(legacy,descriptor,descriptor.Binding)
	if err!=nil{t.Fatal(err)}
	devices,err:=PlanNativeGatewayDevices(top,"rootpool","datapool")
	if err!=nil{t.Fatal(err)}
	pair,err:=PlanNativeGatewayInstances(top,devices,"pg-image","llm-image")
	if err!=nil{t.Fatal(err)}
	_,reader:=instanceReaderFixture(t)
	attachment.volume.Name=top.DatabaseVolume
	attachment.volume.Config["user.aginctus.environment-id"]=top.EnvironmentID
	return top,devices,pair,&fakeGatewayStoppedClient{
		fakeGatewayInstanceReader:reader,fakeGatewayAttachmentReader:attachment,
	}
}

func TestEnvironmentOwnedGatewayStoppedCreationAndObservation(t *testing.T){
	top,devices,pair,client:=environmentGatewayCreateFixture(t)
	if err:=ObserveNativeGatewayAttachmentsWithReader(context.Background(),top,devices,client);err!=nil{t.Fatal(err)}
	result,err:=CreateStoppedGatewayWithClient(context.Background(),top,devices,pair,client,func(context.Context)error{return nil})
	if err!=nil || len(result.Created)!=2 ||
		result.Created[0]!=pair.Postgres.Name || result.Created[1]!=pair.LiteLLM.Name{
		t.Fatalf("environment gateway creation failed: %+v %v",result,err)
	}
	for _,spec:=range []NativeGatewayInstanceSpec{pair.Postgres,pair.LiteLLM}{
		if got:=client.instances[spec.Name].Config["user.aginctus.environment-id"];got!=top.EnvironmentID{
			t.Fatalf("service environment marker changed: %s %s",spec.Name,got)
		}
	}
	observed,err:=ObserveNativeGatewayInstancesWithReader(context.Background(),pair,client)
	if err!=nil || len(observed)!=2 || observed[0].Status!=NativeGatewayCompatible ||
		observed[1].Status!=NativeGatewayCompatible {t.Fatalf("environment observations: %+v %v",observed,err)}
	repeat,err:=CreateStoppedGatewayWithClient(context.Background(),top,devices,pair,client,func(context.Context)error{return nil})
	if err!=nil || len(repeat.Created)!=0 {t.Fatalf("repeat adopted or rewrote gateway: %+v %v",repeat,err)}
}

func TestEnvironmentGatewayRejectsSameProjectCrossEnvironmentResources(t *testing.T){
	top,devices,pair,client:=environmentGatewayCreateFixture(t)
	client.volume.Config["user.aginctus.environment-id"]="123e4567-e89b-42d3-a456-426614174001"
	if err:=ObserveNativeGatewayAttachmentsWithReader(context.Background(),top,devices,client);err==nil{
		t.Fatal("another environment's project-scoped volume accepted")
	}
	client.volume.Config["user.aginctus.environment-id"]=top.EnvironmentID
	_,err:=CreateStoppedGatewayWithClient(context.Background(),top,devices,pair,client,func(context.Context)error{return nil})
	if err!=nil{t.Fatal(err)}
	client.instances[pair.Postgres.Name].Config["user.aginctus.environment-id"]="123e4567-e89b-42d3-a456-426614174001"
	if _,err:=ObserveNativeGatewayInstancesWithReader(context.Background(),pair,client);err==nil{
		t.Fatal("another environment's project-scoped instance accepted")
	}
	if _,err:=CreateStoppedGatewayWithClient(context.Background(),top,devices,pair,client,func(context.Context)error{return nil});err==nil{
		t.Fatal("foreign project-sharing instance adopted")
	}
}

func TestEnvironmentGatewayRejectsLegacyInstanceAndVolumeNames(t *testing.T){
	top,devices,pair,client:=environmentGatewayCreateFixture(t)
	pair.Postgres.Name="postgres-1"
	if _,err:=PlanNativeGatewayCreate(pair,[]NativeGatewayInstanceObservation{
		{Name:"postgres-1",Project:top.Project,Status:NativeGatewayAbsent},
		{Name:pair.LiteLLM.Name,Project:top.Project,Status:NativeGatewayAbsent},
	});err==nil{
		t.Fatal("legacy guest name accepted for environment-owned gateway")
	}
	_ = devices
	_ = client
}

func TestEnvironmentGatewayReadOnlyStatusAndRuntimePreflight(t *testing.T){
	top,devices,pair,client:=environmentGatewayCreateFixture(t)
	if _,err:=CreateStoppedGatewayWithClient(context.Background(),top,devices,pair,client,func(context.Context)error{return nil});err!=nil{
		t.Fatal(err)
	}
	status,err:=InspectNativeGatewayStatusWithReader(context.Background(),top,devices,pair,client)
	if err!=nil || !status.AllStopped {t.Fatalf("owned stopped status: %+v %v",status,err)}
	inspection,err:=InspectNativeGatewayStartupWithReader(context.Background(),top,devices,pair,client)
	if err!=nil || len(inspection.Missing)!=7 || inspection.RuntimeKeysPresent {
		t.Fatalf("unconfigured environment startup preflight: %+v %v",inspection,err)
	}
	instances:=[]*api.Instance{client.instances[pair.Postgres.Name],client.instances[pair.LiteLLM.Name]}
	audit,err:=AuditNativeGatewaySecretExposure(context.Background(),pair,instances)
	if err!=nil || audit.SafeForNativeStart {t.Fatalf("credential exposure audit: %+v %v",audit,err)}
	instances[1].Config["user.aginctus.environment-id"]="123e4567-e89b-42d3-a456-426614174001"
	if _,err:=AssessNativeGatewayRuntimeConfig(context.Background(),pair,instances);err==nil {
		t.Fatal("foreign environment accepted by runtime preflight")
	}
	if _,err:=AuditNativeGatewaySecretExposure(context.Background(),pair,instances);err==nil{
		t.Fatal("foreign environment accepted by credential exposure audit")
	}
}
