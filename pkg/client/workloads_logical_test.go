package client

import(
	"context"
	"errors"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
)
const fakeEnvID="11111111-1111-4111-8111-111111111111"
type fakeLogicalReader struct {calls int;foreign bool}
func (r *fakeLogicalReader) ListWorkloads(_ context.Context,req api.ListWorkloadsRequest)(api.WorkloadList,error){
	r.calls++
	env:=req.EnvironmentID
	if r.foreign{env="22222222-2222-4222-8222-222222222222"}
	return api.WorkloadList{TypeMeta:api.NewTypeMeta("WorkloadList"),EnvironmentID:env,Items:[]api.Workload{{Name:"gateway",EnvironmentID:env,Class:api.WorkloadClassGateway,Implementation:"litellm",Components:[]api.WorkloadComponent{{Name:"gateway"},{Name:"postgres"}}}}},nil
}
func (r *fakeLogicalReader) GetWorkload(_ context.Context,req api.GetWorkloadRequest)(api.WorkloadDetail,error){
	r.calls++
	env:=req.EnvironmentID
	if r.foreign{env="22222222-2222-4222-8222-222222222222"}
	return api.WorkloadDetail{TypeMeta:api.NewTypeMeta("Workload"),Workload:api.Workload{Name:req.Name,EnvironmentID:env,Class:api.WorkloadClassGateway,Implementation:"litellm",Components:[]api.WorkloadComponent{}}},nil
}
func (r *fakeLogicalReader) WorkloadStatus(_ context.Context,req api.WorkloadStatusRequest)(api.WorkloadStatus,error){
	r.calls++
	env:=req.EnvironmentID
	if r.foreign{env="22222222-2222-4222-8222-222222222222"}
	return api.WorkloadStatus{TypeMeta:api.NewTypeMeta("WorkloadStatus"),EnvironmentID:env,Name:req.Name,Observed:[]api.ComponentObservation{}},nil
}
func TestLogicalClientDoesNotInferProjectOrEnvironment(t *testing.T){
	reader:=&fakeLogicalReader{}
	s:=New(Options{LogicalWorkloadReader:reader}).Workloads
	if _,err:=s.List(context.Background(),api.ListWorkloadsRequest{EnvironmentID:fakeEnvID});err==nil||reader.calls!=0{t.Fatalf("unscoped list: %v",err)}
	if _,err:=s.Get(context.Background(),api.GetWorkloadRequest{Project:"aginctus",EnvironmentID:fakeEnvID});err==nil||reader.calls!=0{t.Fatalf("unnamed get: %v",err)}
	if _,err:=s.Status(context.Background(),api.WorkloadStatusRequest{Project:"aginctus",EnvironmentID:fakeEnvID});err==nil||reader.calls!=0{t.Fatalf("unnamed status: %v",err)}
	req:=api.ListWorkloadsRequest{Project:"aginctus",EnvironmentID:fakeEnvID,Class:api.WorkloadClassGateway}
	list,err:=s.List(context.Background(),req)
	if err!=nil||len(list.Items)!=1||len(list.Items[0].Components)!=2{t.Fatalf("list=%#v err=%v",list,err)}
	get,err:=s.Get(context.Background(),api.GetWorkloadRequest{Project:"aginctus",EnvironmentID:fakeEnvID,Name:"gateway"})
	if err!=nil||get.Kind!="Workload"{t.Fatalf("get=%#v err=%v",get,err)}
	status,err:=s.Status(context.Background(),api.WorkloadStatusRequest{Project:"aginctus",EnvironmentID:fakeEnvID,Name:"gateway"})
	if err!=nil||status.Kind!="WorkloadStatus"{t.Fatalf("status=%#v err=%v",status,err)}
	reader.foreign=true
	if _,err:=s.List(context.Background(),req);err==nil{t.Fatal("foreign list escaped")}
	if _,err:=s.Get(context.Background(),api.GetWorkloadRequest{Project:"aginctus",EnvironmentID:fakeEnvID,Name:"gateway"});err==nil{t.Fatal("foreign get escaped")}
	if _,err:=s.Status(context.Background(),api.WorkloadStatusRequest{Project:"aginctus",EnvironmentID:fakeEnvID,Name:"gateway"});err==nil{t.Fatal("foreign status escaped")}
}
func TestLogicalClientCancellationAndMissingBackend(t *testing.T){
	s:=New(Options{}).Workloads
	if _,err:=s.List(context.Background(),api.ListWorkloadsRequest{Project:"aginctus",EnvironmentID:fakeEnvID});err==nil{t.Fatal("missing backend accepted")}
	reader:=&fakeLogicalReader{}
	s=New(Options{LogicalWorkloadReader:reader}).Workloads
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	_,err:=s.List(ctx,api.ListWorkloadsRequest{Project:"aginctus",EnvironmentID:fakeEnvID})
	if !errors.Is(err,context.Canceled)||reader.calls!=0{t.Fatalf("cancel: %v",err)}
}
