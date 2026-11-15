package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration/native"
	"github.com/lxc/incus/v7/shared/api"
)

type gatewayStopFake struct {
	*fakeGatewayStoppedClient
	transitions []string
	failOn string
}
func (f *gatewayStopFake) UpdateInstanceState(name string,req api.InstanceStatePut,etag string)(native.InstanceOperation,error){
	f.transitions=append(f.transitions,name)
	if name==f.failOn {return nil,errors.New("synthetic stop failure")}
	if req.Action!="stop" || req.Force {return nil,errors.New("unexpected forced or start operation")}
	f.instances[name].Status="Stopped"
	return successfulGatewayCreateOperation{},nil
}
func stopFixture(t *testing.T)(NativeGatewayTopology,NativeGatewayDevices,NativeGatewayInstancePair,*gatewayStopFake) {
	t.Helper()
	top,devices,pair,client:=stoppedClientFixture(t)
	_,err:=CreateStoppedGatewayWithClient(context.Background(),top,devices,pair,client,func(context.Context)error{return nil})
	if err!=nil {t.Fatal(err)}
	for _,s:=range []string{"postgres-1","litellm-1"} {client.instances[s].Status="Running"}
	return top,devices,pair,&gatewayStopFake{fakeGatewayStoppedClient:client}
}
func TestGatewayStopReverseOrder(t *testing.T){
	top,devices,pair,client:=stopFixture(t)
	guards:=0
	result,err:=StopNativeGatewayWithClient(context.Background(),top,devices,pair,client,func(context.Context)error{guards++;return nil})
	if err!=nil {t.Fatal(err)}
	if len(result.Changed)!=2 || result.Changed[0]!="litellm-1" || result.Changed[1]!="postgres-1" ||
		len(result.Verified)!=2 || guards!=2 {t.Fatalf("stop ordering: %+v guards=%d",result,guards)}
	result,err=StopNativeGatewayWithClient(context.Background(),top,devices,pair,client,func(context.Context)error{t.Fatal("unexpected no-op guard");return nil})
	if err!=nil || len(result.Changed)!=0 || len(result.Verified)!=2 {t.Fatalf("repeat stop: %+v %v",result,err)}
}
func TestGatewayStopNilGuardAndPartialFailure(t *testing.T){
	top,devices,pair,client:=stopFixture(t)
	if _,err:=StopNativeGatewayWithClient(context.Background(),top,devices,pair,client,nil);err==nil || len(client.transitions)!=0 {
		t.Fatal("nil guard permitted stop")
	}
	client.failOn="postgres-1"
	result,err:=StopNativeGatewayWithClient(context.Background(),top,devices,pair,client,func(context.Context)error{return nil})
	if err==nil || len(result.Changed)!=1 || result.Changed[0]!="litellm-1" ||
		client.instances["postgres-1"].Status!="Running" {t.Fatalf("partial stop not reported: %+v %v",result,err)}
}
