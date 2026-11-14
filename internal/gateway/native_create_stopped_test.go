package gateway

import (
	"context"
	"fmt"
	"testing"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

// Synthetic combined fake is stricter than a no-op operation: every create
// changes its observed inventory and carries the same owned device markers.
type fakeGatewayStoppedClient struct {
	*fakeGatewayInstanceReader
	*fakeGatewayAttachmentReader
	created []string
	onCreate func(string)error
}
func (f *fakeGatewayStoppedClient) GetConnectionInfo()(*incusclient.ConnectionInfo,error){
	return f.fakeGatewayInstanceReader.GetConnectionInfo()
}
func (f *fakeGatewayStoppedClient) GetProject(s string)(*api.Project,string,error){
	return f.fakeGatewayInstanceReader.GetProject(s)
}
type successfulGatewayCreateOperation struct{}
func (successfulGatewayCreateOperation) WaitContext(context.Context)error{return nil}
func (f *fakeGatewayStoppedClient) CreateInstance(req api.InstancesPost)(native.InstanceCreateOperation,error){
	if f.onCreate!=nil { if err:=f.onCreate(req.Name);err!=nil{return nil,err} }
	f.created=append(f.created,req.Name)
	fingerprint:=req.Source.Fingerprint
	f.instances[req.Name]=&api.Instance{
		Name:req.Name,Project:f.info.Project,Type:api.InstanceTypeContainer,Status:"Stopped",
		InstancePut:api.InstancePut{
			Description:req.Description,Devices:req.Devices,Profiles:req.Profiles,
			Config:map[string]string{
				"user.aginctus.managed":"true","user.aginctus.resource":"gateway-instance",
				"user.aginctus.role":req.Config["user.aginctus.role"],
				"user.aginctus.deployment":req.Config["user.aginctus.deployment"],
				"volatile.base_image":fingerprint,
			},
		},
	}
	return successfulGatewayCreateOperation{},nil
}
func stoppedClientFixture(t *testing.T)(NativeGatewayTopology,NativeGatewayDevices,NativeGatewayInstancePair,*fakeGatewayStoppedClient){
	t.Helper()
	top,devices,att:=attachmentFixture(t)
	pair,reader:=instanceReaderFixture(t)
	return top,devices,pair,&fakeGatewayStoppedClient{fakeGatewayInstanceReader:reader,fakeGatewayAttachmentReader:att}
}
func TestCreateStoppedGatewayGuardedOrdered(t *testing.T){
	top,devices,pair,client:=stoppedClientFixture(t)
	guards:=0
	result,err:=CreateStoppedGatewayWithClient(context.Background(),top,devices,pair,client,func(context.Context)error{guards++;return nil})
	if err!=nil {t.Fatal(err)}
	if len(result.Created)!=2 || result.Created[0]!="postgres-1" || result.Created[1]!="litellm-1" ||
		len(result.Verified)!=2 || guards!=2 {t.Fatalf("ordered result: %+v guards=%d",result,guards)}
	result,err=CreateStoppedGatewayWithClient(context.Background(),top,devices,pair,client,func(context.Context)error{guards++;return nil})
	if err!=nil || len(result.Created)!=0 || len(result.Verified)!=2 {t.Fatalf("repeat: %+v %v",result,err)}
}
func TestCreateStoppedGatewayRejectsNoAuthorityAndDrift(t *testing.T){
	top,devices,pair,client:=stoppedClientFixture(t)
	if _,err:=CreateStoppedGatewayWithClient(context.Background(),top,devices,pair,client,nil);err==nil||len(client.created)!=0 {
		t.Fatal("nil guard permitted a write")
	}
	// Caller cannot smuggle an arbitrary extra host mount.
	pair.Postgres.Devices["host"]=map[string]string{"type":"disk","source":"/etc","path":"/mnt"}
	if _,err:=CreateStoppedGatewayWithClient(context.Background(),top,devices,pair,client,func(context.Context)error{return nil});err==nil {
		t.Fatal("unapproved attachment accepted")
	}
}
func TestCreateStoppedGatewayStopsAfterPartialFailure(t *testing.T){
	top,devices,pair,client:=stoppedClientFixture(t)
	client.onCreate=func(name string)error{if name=="litellm-1"{return fmt.Errorf("synthetic submission failure")};return nil}
	result,err:=CreateStoppedGatewayWithClient(context.Background(),top,devices,pair,client,func(context.Context)error{return nil})
	if err==nil || len(result.Created)!=1 || result.Created[0]!="postgres-1" || len(result.Verified)!=1 {
		t.Fatalf("partial completion: %+v %v",result,err)
	}
	if client.instances["postgres-1"]==nil || client.instances["litellm-1"]!=nil {
		t.Fatal("unexpected rollback or hidden second create")
	}
}
