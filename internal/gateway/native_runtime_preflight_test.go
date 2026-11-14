package gateway

import (
	"context"
	"strings"
	"testing"

	"github.com/lxc/incus/v7/shared/api"
)
func runtimeAssessmentFixture(t *testing.T)(NativeGatewayInstancePair,[]*api.Instance){
	t.Helper()
	pair,_:=instanceReaderFixture(t)
	instances:=[]*api.Instance{
		{Name:"postgres-1",Project:pair.Postgres.Project,Status:"Stopped",InstancePut:api.InstancePut{Config:map[string]string{
			"user.aginctus.managed":"true","user.aginctus.resource":"gateway-instance",
			"user.aginctus.role":"postgres","user.aginctus.deployment":pair.Postgres.Project,
		}}},
		{Name:"litellm-1",Project:pair.LiteLLM.Project,Status:"Stopped",InstancePut:api.InstancePut{Config:map[string]string{
			"user.aginctus.managed":"true","user.aginctus.resource":"gateway-instance",
			"user.aginctus.role":"litellm","user.aginctus.deployment":pair.LiteLLM.Project,
		}}},
	}
	return pair,instances
}
func TestNativeGatewayRuntimeMissingAfterStoppedCreate(t *testing.T){
	pair,instances:=runtimeAssessmentFixture(t)
	got,err:=AssessNativeGatewayRuntimeConfig(context.Background(),pair,instances)
	if err!=nil || got.Complete || len(got.Missing)!=7 {t.Fatalf("missing startup prerequisites: %+v %v",got,err)}
}
func TestNativeGatewayRuntimeNeverLeaksSecretValues(t *testing.T){
	pair,instances:=runtimeAssessmentFixture(t)
	secret:="distinctive-synthetic-password"
	for _,req:=range NativeGatewayRuntimeRequirements(){
		idx:=0
		if req.Service=="litellm" {idx=1}
		value:="synthetic"
		if req.Sensitive {value=secret}
		instances[idx].Config["environment."+req.Key]=value
	}
	got,err:=AssessNativeGatewayRuntimeConfig(context.Background(),pair,instances)
	if err!=nil || !got.Complete || strings.Contains(strings.Join(got.Missing,","),secret) {t.Fatalf("unexpected assessment: %+v %v",got,err)}
	instances[1].Config["environment.DATABASE_URL"]="${UNRESOLVED_PASSWORD}"
	got,err=AssessNativeGatewayRuntimeConfig(context.Background(),pair,instances)
	if err!=nil || got.Complete || len(got.Missing)!=1 || got.Missing[0]!="litellm.DATABASE_URL" ||
		strings.Contains(strings.Join(got.Missing,","),secret) {t.Fatalf("unresolved credential accepted: %+v %v",got,err)}
}
func TestNativeGatewayRuntimeRefusesWrongIdentityAndRunning(t *testing.T){
	pair,instances:=runtimeAssessmentFixture(t)
	instances[0].Status="Running"
	if _,err:=AssessNativeGatewayRuntimeConfig(context.Background(),pair,instances);err==nil {t.Fatal("running service accepted")}
	instances[0].Status="Stopped"
	instances[1].Config["user.aginctus.resource"]="compose-instance"
	if _,err:=AssessNativeGatewayRuntimeConfig(context.Background(),pair,instances);err==nil {t.Fatal("legacy instance accepted")}
}
