package client

import (
	"context"
	"errors"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
)

type fakeScopedWorkloadReader struct {
	request api.ScopedWorkloadStatusRequest
	calls int
}

func (f *fakeScopedWorkloadReader) ReadScopedWorkloadStatus(_ context.Context, req api.ScopedWorkloadStatusRequest) (api.ScopedWorkloadStatusList,error) {
	f.calls++
	f.request = req
	return api.ScopedWorkloadStatusList{TypeMeta:api.NewTypeMeta("ScopedWorkloadStatusList"),Project:req.Project,Items:[]api.ScopedWorkloadStatus{}},nil
}

func TestScopedStatusRequiresExplicitProjectAndDelegates(t *testing.T) {
	fake:=&fakeScopedWorkloadReader{}
	c:=New(Options{ScopedWorkloadReader:fake})
	if _,err:=c.Workloads.ScopedStatus(context.Background(),api.ScopedWorkloadStatusRequest{});err==nil || fake.calls!=0 {
		t.Fatalf("missing project called backend: %v",err)
	}
	result,err:=c.Workloads.ScopedStatus(context.Background(),api.ScopedWorkloadStatusRequest{Project:"aginctus",RunningOnly:true})
	if err!=nil || result.Project!="aginctus" || !fake.request.RunningOnly || fake.calls!=1 { t.Fatalf("result=%#v err=%v calls=%d",result,err,fake.calls) }
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	_,err=c.Workloads.ScopedStatus(ctx,api.ScopedWorkloadStatusRequest{Project:"aginctus"})
	if !errors.Is(err,context.Canceled)||fake.calls!=1 { t.Fatalf("cancellation err=%v calls=%d",err,fake.calls) }
	if _,err:=New(Options{}).Workloads.ScopedStatus(context.Background(),api.ScopedWorkloadStatusRequest{Project:"aginctus"});err==nil {t.Fatal("missing provider accepted")}
}
