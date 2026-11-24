package cli

import(
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
)
const readTestEnv="11111111-1111-4111-8111-111111111111"
type testEnrolledReader struct{}
func (testEnrolledReader) ListEnvironments(_ context.Context,project string)(api.EnvironmentList,error){
	return api.EnvironmentList{TypeMeta:api.NewTypeMeta("EnvironmentList"),Items:[]api.Environment{{ID:readTestEnv,Name:"dev",Binding:api.EnvironmentBinding{ServerID:"verified-server",Project:project}}}},nil
}
func (testEnrolledReader) GetEnvironment(_ context.Context,project,id string)(api.EnvironmentDetail,error){
	return api.EnvironmentDetail{TypeMeta:api.NewTypeMeta("Environment"),Environment:api.Environment{ID:id,Name:"dev",Binding:api.EnvironmentBinding{ServerID:"verified-server",Project:project}}},nil
}
func TestInjectedAuthenticatedEnvironmentCLI(t *testing.T){
	for _,tc:=range []struct{args []string;expect string}{
		{[]string{"--incus-project","aginctus","environment","list","-o","json"},`"kind": "EnvironmentList"`},
		{[]string{"environment","list","--incus-project","aginctus","-o","wide"},"SERVER ID"},
		{[]string{"environment","show",readTestEnv,"--incus-project","aginctus","-o","yaml"},"kind: Environment"},
		{[]string{"environment","list","--incus-project","aginctus","-o","name"},readTestEnv},
	}{
		var out,errs bytes.Buffer
		code:=RunWithAuthorizedReaders(context.Background(),tc.args,&out,&errs,nil,nil,nil,testEnrolledReader{},nil)
		if code!=0||errs.Len()!=0||!strings.Contains(out.String(),tc.expect){t.Fatalf("args=%v code=%d out=%q err=%q",tc.args,code,out.String(),errs.String())}
		if strings.Contains(strings.Join(tc.args," "),"-o json"){
			var doc api.EnvironmentList
			if err:=json.Unmarshal(out.Bytes(),&doc);err!=nil||len(doc.Items)!=1||doc.Items[0].Binding.Project!="aginctus"{t.Fatalf("doc=%#v err=%v",doc,err)}
		}
	}
}
func TestEnvironmentCLIRequiresAuthenticatedBackend(t *testing.T){
	for _,args:=range [][]string{
		{"environment","list","--incus-project","aginctus","-o","json"},
		{"environment","show",readTestEnv,"--incus-project","aginctus","-o","yaml"},
		{"environment","list","-o","json"},
	}{
		var out,errs bytes.Buffer
		code:=Run(context.Background(),args,&out,&errs,nil,nil)
		if code==0||out.Len()!=0 {t.Fatalf("args=%v code=%d out=%q err=%q",args,code,out.String(),errs.String())}
	}
}

func TestEnvironmentShowResolvesUniqueNameThroughVerifiedInventory(t *testing.T) {
	var out,errs bytes.Buffer
	code:=RunWithAuthorizedReaders(context.Background(),
		[]string{"--incus-project","team","environment","show","dev","-o","json"},
		&out,&errs,nil,nil,nil,testEnrolledReader{},nil)
	if code!=0||errs.Len()!=0||!strings.Contains(out.String(),readTestEnv)||
		!strings.Contains(out.String(),`"kind": "Environment"`) {
		t.Fatalf("show name: code=%d out=%q stderr=%q",code,out.String(),errs.String())
	}
}

func TestEnvironmentShowRejectsAmbiguousAndStaleEnrollment(t *testing.T) {
	env:=api.Environment{ID:readTestEnv,Name:"dev",Binding:api.EnvironmentBinding{ServerID:"srv",Project:"team"}}
	other:=env
	other.ID="22222222-2222-4222-8222-222222222222"
	for _,tc:=range []struct{name string;reader nameSelectionReader}{
		{"ambiguous",nameSelectionReader{list:[]api.Environment{env,other},detail:env}},
		{"stale",nameSelectionReader{list:[]api.Environment{env},detail:other}},
	} {
		t.Run(tc.name,func(t *testing.T){
			var out,errs bytes.Buffer
			code:=RunWithAuthorizedReaders(context.Background(),
				[]string{"--incus-project","team","environment","show","dev","-o","json"},
				&out,&errs,nil,nil,nil,tc.reader,nil)
			if code==0||out.Len()!=0||errs.Len()==0 {
				t.Fatalf("unsafe selection: code=%d out=%q stderr=%q",code,out.String(),errs.String())
			}
		})
	}
}
