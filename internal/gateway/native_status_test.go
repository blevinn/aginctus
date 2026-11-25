package gateway

import (
	"context"
	"testing"
)
func TestNativeGatewayStatusReadOnly(t *testing.T){
	top,devices,pair,client:=stoppedClientFixture(t)
	_,err:=CreateStoppedGatewayWithClient(context.Background(),top,devices,pair,client.imagesForCreate,client,func(context.Context)error{return nil})
	if err!=nil {t.Fatal(err)}
	status,err:=InspectNativeGatewayStatusWithReader(context.Background(),top,devices,pair,client)
	if err!=nil || !status.AllStopped || status.AllRunning || len(status.Services)!=2 {
		t.Fatalf("unexpected stopped status: %+v %v",status,err)
	}
	client.instances["postgres-1"].Status="Running"
	status,err=InspectNativeGatewayStatusWithReader(context.Background(),top,devices,pair,client)
	if err!=nil || status.AllStopped || status.AllRunning || status.Services[0].State!=NativeGatewayServiceRunning {
		t.Fatalf("mixed status: %+v %v",status,err)
	}
	client.instances["litellm-1"].Status="Running"
	status,err=InspectNativeGatewayStatusWithReader(context.Background(),top,devices,pair,client)
	if err!=nil || !status.AllRunning || status.AllStopped {t.Fatalf("running status: %+v %v",status,err)}
}
func TestNativeGatewayStatusRejectsForeignAndUnknown(t *testing.T){
	top,devices,pair,client:=stoppedClientFixture(t)
	_,err:=CreateStoppedGatewayWithClient(context.Background(),top,devices,pair,client.imagesForCreate,client,func(context.Context)error{return nil})
	if err!=nil {t.Fatal(err)}
	client.instances["postgres-1"].Status="Error"
	if _,err:=InspectNativeGatewayStatusWithReader(context.Background(),top,devices,pair,client);err==nil {
		t.Fatal("transitional lifecycle accepted")
	}
	client.instances["postgres-1"].Status="Stopped"
	client.instances["litellm-1"].Config["user.aginctus.resource"]="compose-instance"
	if _,err:=InspectNativeGatewayStatusWithReader(context.Background(),top,devices,pair,client);err==nil {
		t.Fatal("legacy service accepted")
	}
}
