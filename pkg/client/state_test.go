package client

import (
	"context"
	"errors"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
)

const stateTestEnvironment = "11111111-1111-4111-8111-111111111111"

type testConfiguredSource struct {
	calls int
	value api.ConfiguredWorkloadList
	err   error
}
func (s *testConfiguredSource) GetConfiguredWorkloads(_ context.Context, _, _ string) (api.ConfiguredWorkloadList, error) {
	s.calls++
	return s.value, s.err
}

func TestConfiguredStateRequiresVerifiedScopeAndRejectsForeignResult(t *testing.T) {
	source := &testConfiguredSource{value: api.ConfiguredWorkloadList{
		TypeMeta: api.NewTypeMeta("ConfiguredWorkloadList"),
		EnvironmentID: stateTestEnvironment,
		Layer: "configured", Items: []api.ConfiguredWorkload{
			{ID: "worker", Profiles: []string{"base"}},
		},
	}}
	service := New(Options{ConfiguredStateReader: source}).State
	for _, req := range []GetConfiguredStateRequest{{}, {Project: "aginctus"}, {EnvironmentID: stateTestEnvironment}} {
		if _, err := service.Configured(context.Background(), req); err == nil || source.calls != 0 {
			t.Fatalf("unscoped request reached source: %+v %v", req, err)
		}
	}
	request := GetConfiguredStateRequest{Project: "aginctus", EnvironmentID: stateTestEnvironment}
	value, err := service.Configured(context.Background(), request)
	if err != nil || len(value.Items) != 1 { t.Fatalf("read=%+v err=%v", value, err) }
	for _, alter := range []func(*api.ConfiguredWorkloadList){
		func(v *api.ConfiguredWorkloadList){v.EnvironmentID="foreign"},
		func(v *api.ConfiguredWorkloadList){v.Layer="accepted"},
		func(v *api.ConfiguredWorkloadList){v.Kind="WorkloadList"},
		func(v *api.ConfiguredWorkloadList){v.Items=nil},
		func(v *api.ConfiguredWorkloadList){v.Items=append(v.Items,v.Items[0])},
	} {
		source.value = api.ConfiguredWorkloadList{
			TypeMeta: api.NewTypeMeta("ConfiguredWorkloadList"), EnvironmentID: stateTestEnvironment,
			Layer: "configured", Items: []api.ConfiguredWorkload{{ID:"worker",Profiles:[]string{"base"}}},
		}
		alter(&source.value)
		if got,err:=service.Configured(context.Background(),request);err==nil||len(got.Items)!=0 {t.Fatalf("foreign/inconsistent result leaked: %+v err=%v",got,err)}
	}
	source.err=errors.New("secret-upstream-error")
	if _,err:=service.Configured(context.Background(),request);err==nil||err.Error()=="secret-upstream-error"{t.Fatalf("backend error leaked: %v",err)}
}

func TestConfiguredStateCancellationAndUnavailableSource(t *testing.T){
	req:=GetConfiguredStateRequest{Project:"aginctus",EnvironmentID:stateTestEnvironment}
	if _,err:=New(Options{}).State.Configured(context.Background(),req);err==nil{t.Fatal("missing authority returned empty state")}
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	source:=&testConfiguredSource{}
	if _,err:=New(Options{ConfiguredStateReader:source}).State.Configured(ctx,req);!errors.Is(err,context.Canceled)||source.calls!=0{t.Fatalf("cancelled: err=%v calls=%d",err,source.calls)}
	if _,err:=New(Options{ConfiguredStateReader:source}).State.Configured(nil,req);err==nil{t.Fatal("nil context accepted")}
}
