package gateway

import (
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
)
func gatewayTestEnvironment(id,project string)environment.Descriptor{
	return environment.Descriptor{
		ID:id,Name:"example",
		Binding:environment.Binding{ServerID:"verified-server-1",Project:project},
	}
}
func TestNativeGatewayIdentitySeparatesEnvironmentsWithinOneProject(t *testing.T) {
	first:=gatewayTestEnvironment("123e4567-e89b-42d3-a456-426614174000","operator-project")
	second:=gatewayTestEnvironment("123e4567-e89b-42d3-a456-426614174001","operator-project")
	a,err:=NativeGatewayIdentity(first,first.Binding)
	if err!=nil{t.Fatal(err)}
	b,err:=NativeGatewayIdentity(second,second.Binding)
	if err!=nil{t.Fatal(err)}
	if a.Project!=b.Project || a.PostgresInstance==b.PostgresInstance ||
		a.LiteLLMInstance==b.LiteLLMInstance || a.DatabaseVolume==b.DatabaseVolume {
		t.Fatalf("environment resources collide: %+v %+v",a,b)
	}
	if err:=a.ValidateEnvironmentIdentity(first,first.Binding);err!=nil{t.Fatal(err)}
	c:=a;c.DatabaseVolume=b.DatabaseVolume
	if err:=c.ValidateEnvironmentIdentity(first,first.Binding);err==nil {t.Fatal("foreign volume accepted")}
	c=a;c.PostgresInstance=b.PostgresInstance
	if err:=c.ValidateEnvironmentIdentity(first,first.Binding);err==nil {t.Fatal("foreign guest accepted")}
}
func TestNativeGatewayIdentityRejectsUnverifiedSelection(t *testing.T) {
	env:=gatewayTestEnvironment("123e4567-e89b-42d3-a456-426614174000","operator-project")
	for _,observed:=range []environment.Binding{
		{ServerID:"other-server",Project:"operator-project"},
		{ServerID:"verified-server-1",Project:"other-project"},
		{ServerID:"verified-server-1",Project:"default"},
	}{
		if _,err:=NativeGatewayIdentity(env,observed);err==nil {t.Fatalf("unverified binding accepted: %+v",observed)}
	}
	invalid:=env;invalid.ID="not-a-uuid"
	if _,err:=NativeGatewayIdentity(invalid,invalid.Binding);err==nil {t.Fatal("malformed environment identity accepted")}
}
func TestNativeGatewayIdentityDisplayRenameStable(t *testing.T) {
	env:=gatewayTestEnvironment("123e4567-e89b-42d3-a456-426614174000","operator-project")
	before,_:=NativeGatewayIdentity(env,env.Binding)
	env.Name="renamed"
	after,err:=NativeGatewayIdentity(env,env.Binding)
	if err!=nil || before!=after {t.Fatalf("rename changed resource identities: %+v %+v %v",before,after,err)}
}

func TestBindNativeGatewayTopologyProjectsEnvironmentVolumeAndGuests(t *testing.T) {
	top:=nativePinnedFixture()
	env:=gatewayTestEnvironment("123e4567-e89b-42d3-a456-426614174000",top.Project)
	bound,err:=BindNativeGatewayTopology(top,env,env.Binding)
	if err!=nil{t.Fatal(err)}
	expected,err:=NativeGatewayIdentity(env,env.Binding)
	if err!=nil{t.Fatal(err)}
	if bound.EnvironmentID!=env.ID || bound.DatabaseVolume!=expected.DatabaseVolume ||
		bound.Services[0].Volume!=expected.DatabaseVolume || top.EnvironmentID!="" ||
		top.DatabaseVolume!="gateway-postgres" {
		t.Fatalf("environment binding mutates original or loses identity: %+v",bound)
	}
	devices,err:=PlanNativeGatewayDevices(bound,"rootpool","datapool")
	if err!=nil{t.Fatal(err)}
	if devices.Postgres["database"]["source"]!=expected.DatabaseVolume {t.Fatal("bound storage not attached")}
	pair,err:=PlanNativeGatewayInstances(bound,devices,"pg-image","llm-image")
	if err!=nil || pair.Postgres.Name!=expected.PostgresInstance ||
		pair.LiteLLM.Name!=expected.LiteLLMInstance ||
		pair.Postgres.EnvironmentID!=env.ID || pair.LiteLLM.EnvironmentID!=env.ID {
		t.Fatalf("environment guest identities mismatch: %+v %v",pair,err)
	}
	bound.DatabaseVolume="gateway-postgres"
	if err:=bound.ValidateExecutionShape();err==nil {t.Fatal("bound environment accepted legacy volume")}
}
func TestBindNativeGatewayTopologyRejectsCrossProjectAndSecondBinding(t *testing.T){
	top:=nativePinnedFixture()
	env:=gatewayTestEnvironment("123e4567-e89b-42d3-a456-426614174000",top.Project)
	_,err:=BindNativeGatewayTopology(top,env,environment.Binding{ServerID:env.Binding.ServerID,Project:"other-project"})
	if err==nil{t.Fatal("cross-project bind accepted")}
	bound,err:=BindNativeGatewayTopology(top,env,env.Binding)
	if err!=nil{t.Fatal(err)}
	if _,err:=BindNativeGatewayTopology(bound,env,env.Binding);err==nil {t.Fatal("already-bound topology adopted")}
}
