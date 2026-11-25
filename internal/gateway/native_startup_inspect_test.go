package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeGatewayStartupInspectionMissingRuntime(t *testing.T){
	top,devices,pair,client:=stoppedClientFixture(t)
	_,err:=CreateStoppedGatewayWithClient(context.Background(),top,devices,pair,client.imagesForCreate,client,func(context.Context)error{return nil})
	if err!=nil {t.Fatal(err)}
	inspection,err:=InspectNativeGatewayStartupWithReader(context.Background(),top,devices,pair,client)
	if err!=nil || inspection.RuntimeKeysPresent || len(inspection.Missing)!=7 ||
		len(inspection.Order)!=2 || inspection.Order[0]!="postgres-1" || inspection.Order[1]!="litellm-1" {
		t.Fatalf("startup report: %+v %v",inspection,err)
	}
}
func TestNativeGatewayStartupInspectionRedacted(t *testing.T){
	top,devices,pair,client:=stoppedClientFixture(t)
	_,err:=CreateStoppedGatewayWithClient(context.Background(),top,devices,pair,client.imagesForCreate,client,func(context.Context)error{return nil})
	if err!=nil {t.Fatal(err)}
	const sensitive="unique-synthetic-credential-hex"
	for _,req:=range NativeGatewayRuntimeRequirements(){
		name:="postgres-1"
		if req.Service=="litellm" {name="litellm-1"}
		client.instances[name].Config["environment."+req.Key]=sensitive
	}
	inspection,err:=InspectNativeGatewayStartupWithReader(context.Background(),top,devices,pair,client)
	if err!=nil || !inspection.RuntimeKeysPresent {t.Fatalf("unexpected runtime inspection: %+v %v",inspection,err)}
	raw,err:=json.Marshal(inspection)
	if err!=nil || strings.Contains(string(raw),sensitive) {t.Fatal("secret leaked into inspection report")}
}
func TestNativeGatewayStartupInspectionRejectsForeignAndRunning(t *testing.T){
	top,devices,pair,client:=stoppedClientFixture(t)
	_,err:=CreateStoppedGatewayWithClient(context.Background(),top,devices,pair,client.imagesForCreate,client,func(context.Context)error{return nil})
	if err!=nil {t.Fatal(err)}
	client.instances["litellm-1"].Status="Running"
	if _,err:=InspectNativeGatewayStartupWithReader(context.Background(),top,devices,pair,client);err==nil {t.Fatal("running service accepted")}
	client.instances["litellm-1"].Status="Stopped"
	client.instances["postgres-1"].Config["user.aginctus.resource"]="compose-instance"
	if _,err:=InspectNativeGatewayStartupWithReader(context.Background(),top,devices,pair,client);err==nil {t.Fatal("legacy resource accepted")}
}
