package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)
func TestNativeGatewayCredentialExposureRejectsLegacyEnvironment(t *testing.T){
	pair,instances:=runtimeAssessmentFixture(t)
	const secret="super-sensitive-synthetic-only-key"
	for _,req:=range NativeGatewayRuntimeRequirements(){
		if !req.Sensitive {continue}
		idx:=0;if req.Service=="litellm" {idx=1}
		instances[idx].Config["environment."+req.Key]=secret
	}
	report,err:=AuditNativeGatewaySecretExposure(context.Background(),pair,instances)
	if err!=nil || report.SafeForNativeStart || len(report.ExposedKeys)!=4 {
		t.Fatalf("incorrect sensitive key audit: %+v %v",report,err)
	}
	raw,_:=json.Marshal(report)
	if strings.Contains(string(raw),secret){t.Fatal("sensitive value leaked")}
}
func TestNativeGatewayCredentialExposureNoKeysStillNotAuthorized(t *testing.T){
	pair,instances:=runtimeAssessmentFixture(t)
	report,err:=AuditNativeGatewaySecretExposure(context.Background(),pair,instances)
	if err!=nil || report.SafeForNativeStart || len(report.ExposedKeys)!=0{
		t.Fatalf("missing evidence accepted: %+v %v",report,err)
	}
	instances[1].Config["environment.DATABASE_URL"]="${SECRET}"
	report,err=AuditNativeGatewaySecretExposure(context.Background(),pair,instances)
	if err!=nil || len(report.UnresolvedKeys)!=1 || report.UnresolvedKeys[0]!="litellm.DATABASE_URL" {
		t.Fatalf("unresolved secret reference not reported: %+v %v",report,err)
	}
}
func TestNativeGatewayCredentialExposureRejectsForeign(t *testing.T){
	pair,instances:=runtimeAssessmentFixture(t)
	instances[0].Config["user.aginctus.resource"]="compose-instance"
	if _,err:=AuditNativeGatewaySecretExposure(context.Background(),pair,instances);err==nil {t.Fatal("legacy instance accepted")}
}
