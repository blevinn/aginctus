package client

import (
	"context"
	"errors"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
)

const diffEnvID = "11111111-1111-4111-8111-111111111111"
type testStateDiffSource struct{
	calls int
	value api.ConfiguredAcceptedDiff
	err error
}
func (s *testStateDiffSource) DiffConfiguredAccepted(_ context.Context, _, _ string)(api.ConfiguredAcceptedDiff,error){
	s.calls++
	return s.value,s.err
}
func validStateDiff() api.ConfiguredAcceptedDiff{
	return api.ConfiguredAcceptedDiff{
		TypeMeta:api.NewTypeMeta("ConfiguredAcceptedDiff"),
		Environment:api.Environment{ID:diffEnvID,Name:"dev",Binding:api.EnvironmentBinding{ServerID:"verified",Project:"team"}},
		FromLayer:"accepted",ToLayer:"configured",HasAccepted:false,ProposedDigest:"candidate-digest",
		NewlySelected:[]string{"agent"},Unselected:[]string{},Modified:[]api.AcceptedWorkloadChange{},
	}
}
func TestStateDiffRejectsUnverifiedAndContradictoryResults(t *testing.T){
	source:=&testStateDiffSource{value:validStateDiff()}
	s:=New(Options{ConfiguredAcceptedDiffReader:source}).State
	req:=ReadStateRequest{Project:"team",EnvironmentID:diffEnvID}
	for _,bad:=range []ReadStateRequest{{},{Project:"team"},{EnvironmentID:diffEnvID}}{
		if _,err:=s.Diff(context.Background(),bad);err==nil||source.calls!=0{t.Fatalf("unscoped diff: %+v %v",bad,err)}
	}
	result,err:=s.Diff(context.Background(),req)
	if err!=nil||len(result.NewlySelected)!=1||source.calls!=1{t.Fatalf("result=%+v err=%v",result,err)}
	for _,alter:=range []func(*api.ConfiguredAcceptedDiff){
		func(v *api.ConfiguredAcceptedDiff){v.Environment.ID="foreign"},
		func(v *api.ConfiguredAcceptedDiff){v.Environment.Binding.Project="foreign"},
		func(v *api.ConfiguredAcceptedDiff){v.ToLayer="observed"},
		func(v *api.ConfiguredAcceptedDiff){v.HasAccepted=true},
		func(v *api.ConfiguredAcceptedDiff){v.Modified=nil},
		func(v *api.ConfiguredAcceptedDiff){v.Unselected=[]string{"agent"}},
	}{
		source.value=validStateDiff()
		alter(&source.value)
		got,err:=s.Diff(context.Background(),req)
		if err==nil||got.Kind!=""{t.Fatalf("unverified result escaped %+v err=%v",got,err)}
	}
	source.value=validStateDiff();source.err=errors.New("secret-error")
	if _,err:=s.Diff(context.Background(),req);err==nil||err.Error()=="secret-error"{t.Fatal("backend error disclosed")}
}
func TestStateReadRequiresAuthorityAndHonorsCancellation(t *testing.T){
	req:=ReadStateRequest{Project:"team",EnvironmentID:diffEnvID}
	s:=New(Options{}).State
	if _,err:=s.Accepted(context.Background(),req);err==nil{t.Fatal("no accepted reader")}
	if _,err:=s.Diff(context.Background(),req);err==nil{t.Fatal("no diff reader")}
	source:=&testStateDiffSource{}
	s=New(Options{ConfiguredAcceptedDiffReader:source}).State
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	if _,err:=s.Diff(ctx,req);!errors.Is(err,context.Canceled)||source.calls!=0{t.Fatalf("cancel: %v",err)}
}

func TestStateAcceptedSanitizesBackendErrors(t *testing.T) {
	req:=ReadStateRequest{Project:"team",EnvironmentID:diffEnvID}
	source:=&acceptedStateTestReader{result:goodAcceptedStatus()}
	s:=New(Options{AcceptedStateReader:source}).State
	got,err:=s.Accepted(context.Background(),req)
	if err!=nil||got.Kind!="AcceptedStateStatus"||got.HasAccepted{t.Fatalf("got=%+v err=%v",got,err)}
	source.err=errors.New("sensitive credential or storage details")
	_,err=s.Accepted(context.Background(),req)
	if err==nil||err.Error()=="sensitive credential or storage details"{
		t.Fatalf("raw accepted-state error escaped: %v",err)
	}
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	if _,err:=s.Accepted(ctx,req);!errors.Is(err,context.Canceled){t.Fatalf("cancellation lost: %v",err)}
}
