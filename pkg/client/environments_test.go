package client

import(
	"context"
	"errors"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
)
type testEnvironmentReader struct {calls int}
func (r *testEnvironmentReader) ListEnvironments(_ context.Context,project string)(api.EnvironmentList,error){
	r.calls++
	return api.EnvironmentList{TypeMeta:api.NewTypeMeta("EnvironmentList"),Items:[]api.Environment{}},nil
}
func (r *testEnvironmentReader) GetEnvironment(_ context.Context,project,id string)(api.EnvironmentDetail,error){
	r.calls++
	return api.EnvironmentDetail{TypeMeta:api.NewTypeMeta("Environment"),Environment:api.Environment{ID:id}},nil
}
func TestEnvironmentClientRequiresExplicitAuthenticatedScope(t *testing.T){
	reader:=&testEnvironmentReader{}
	service:=New(Options{EnvironmentReader:reader}).Environments
	if _,err:=service.List(context.Background(),ListEnvironmentsRequest{});err==nil||reader.calls!=0 {t.Fatalf("unexpected list: %v",err)}
	if _,err:=service.Get(context.Background(),GetEnvironmentRequest{Project:"aginctus"});err==nil||reader.calls!=0 {t.Fatalf("unexpected get: %v",err)}
	list,err:=service.List(context.Background(),ListEnvironmentsRequest{Project:"aginctus"})
	if err!=nil||list.Items==nil||reader.calls!=1 {t.Fatalf("list=%#v err=%v",list,err)}
	detail,err:=service.Get(context.Background(),GetEnvironmentRequest{Project:"aginctus",ID:"11111111-1111-4111-8111-111111111111"})
	if err!=nil||detail.Kind!="Environment"||reader.calls!=2 {t.Fatalf("detail=%#v err=%v",detail,err)}
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	if _,err:=service.List(ctx,ListEnvironmentsRequest{Project:"aginctus"});!errors.Is(err,context.Canceled)||reader.calls!=2 {t.Fatalf("cancel: %v",err)}
	if _,err:=New(Options{}).Environments.List(context.Background(),ListEnvironmentsRequest{Project:"aginctus"});err==nil {t.Fatal("no provider accepted")}
}
