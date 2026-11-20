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
