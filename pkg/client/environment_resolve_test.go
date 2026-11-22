package client

import (
	"context"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
)

type resolveTestReader struct {
	items []api.Environment
	detail api.Environment
	listCalls int
	getCalls int
}
func (r *resolveTestReader) ListEnvironments(context.Context,string)(api.EnvironmentList,error){
	r.listCalls++
	return api.EnvironmentList{TypeMeta:api.NewTypeMeta("EnvironmentList"),Items:r.items},nil
}
func (r *resolveTestReader) GetEnvironment(_ context.Context,_,id string)(api.EnvironmentDetail,error){
	r.getCalls++
	return api.EnvironmentDetail{TypeMeta:api.NewTypeMeta("Environment"),Environment:r.detail},nil
}

func TestEnvironmentNameResolveRequiresUniqueVerifiedIdentity(t *testing.T){
	one:=api.Environment{ID:"11111111-1111-4111-8111-111111111111",Name:"dev",Binding:api.EnvironmentBinding{ServerID:"srv",Project:"team"}}
	two:=api.Environment{ID:"22222222-2222-4222-8222-222222222222",Name:"prod",Binding:one.Binding}
	r:=&resolveTestReader{items:[]api.Environment{two,one},detail:one}
	s:=New(Options{EnvironmentReader:r}).Environments
	result,err:=s.Resolve(context.Background(),ResolveEnvironmentRequest{Project:"team",Selector:"dev"})
	if err!=nil||result.ID!=one.ID||r.listCalls!=1||r.getCalls!=1{t.Fatalf("resolve=%+v err=%v calls=%d/%d",result,err,r.listCalls,r.getCalls)}
	result,err=s.Resolve(context.Background(),ResolveEnvironmentRequest{Project:"team",Selector:one.ID})
	if err!=nil||result.ID!=one.ID||r.listCalls!=1||r.getCalls!=2{t.Fatalf("UUID resolution=%+v err=%v",result,err)}
	r.items[1].Name="prod"
	if value,err:=s.Resolve(context.Background(),ResolveEnvironmentRequest{Project:"team",Selector:"prod"});err==nil||value.Kind!=""||r.getCalls!=2{
		t.Fatalf("ambiguous name allowed: %+v err=%v",value,err)
	}
	r.items=[]api.Environment{one}
	r.detail.Binding.Project="other"
	if value,err:=s.Resolve(context.Background(),ResolveEnvironmentRequest{Project:"team",Selector:"dev"});err==nil||value.ID!=""{
		t.Fatalf("foreign detail returned: %+v err=%v",value,err)
	}
}
func TestEnvironmentNameResolveDeniesUnscopedOrStaleInventory(t *testing.T){
	one:=api.Environment{ID:"11111111-1111-4111-8111-111111111111",Name:"dev",Binding:api.EnvironmentBinding{ServerID:"srv",Project:"team"}}
	r:=&resolveTestReader{items:[]api.Environment{one},detail:one}
	s:=New(Options{EnvironmentReader:r}).Environments
	for _,req:=range []ResolveEnvironmentRequest{{},{Project:"team"},{Selector:"dev"}}{
		if _,err:=s.Resolve(context.Background(),req);err==nil||r.listCalls!=0{t.Fatalf("unscoped read: %+v err=%v",req,err)}
	}
	r.detail.Name="renamed"
	if value,err:=s.Resolve(context.Background(),ResolveEnvironmentRequest{Project:"team",Selector:"dev"});err==nil||value.ID!=""{
		t.Fatalf("stale identity returned: %+v err=%v",value,err)
	}
}
