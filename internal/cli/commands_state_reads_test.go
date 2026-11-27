package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
)

const stateCLIEnv = "11111111-1111-4111-8111-111111111111"

type stateCLIFixture struct{}
func (stateCLIFixture) GetConfiguredWorkloads(context.Context,string,string)(api.ConfiguredWorkloadList,error){
	return api.ConfiguredWorkloadList{TypeMeta:api.NewTypeMeta("ConfiguredWorkloadList"),
		EnvironmentID:stateCLIEnv,Layer:"configured",Items:[]api.ConfiguredWorkload{{ID:"worker",Profiles:[]string{"base"}}}},nil
}
func (stateCLIFixture) GetAcceptedState(context.Context,string,string)(api.AcceptedStateStatus,error){
	return api.AcceptedStateStatus{TypeMeta:api.NewTypeMeta("AcceptedStateStatus"),Layer:"accepted",
		Environment:api.Environment{ID:stateCLIEnv,Name:"dev",Binding:api.EnvironmentBinding{ServerID:"srv",Project:"team"}},
		HasAccepted:false},nil
}
func (stateCLIFixture) DiffConfiguredAccepted(context.Context,string,string)(api.ConfiguredAcceptedDiff,error){
	return api.ConfiguredAcceptedDiff{TypeMeta:api.NewTypeMeta("ConfiguredAcceptedDiff"),
		Environment:api.Environment{ID:stateCLIEnv,Name:"dev",Binding:api.EnvironmentBinding{ServerID:"srv",Project:"team"}},
		FromLayer:"accepted",ToLayer:"configured",ProposedDigest:"sha256:abc",
		HasAccepted:false,NewlySelected:[]string{"worker"},Unselected:[]string{},Modified:[]api.AcceptedWorkloadChange{},
		Changed:true},nil
}
func TestStateCLIReadOnlyTypedOutput(t *testing.T){
	fixture:=stateCLIFixture{}
	for _,tc:=range []struct{args []string;expected string}{
		{[]string{"state","show","-o","json"},`"kind": "AcceptedStateStatus"`},
		{[]string{"state","show","--layer","configured","-o","json"},`"kind": "ConfiguredWorkloadList"`},
		{[]string{"state","diff","-o","yaml"},"fromLayer: accepted"},
		{[]string{"state","diff","-o","table"},"NEWLY SELECTED"},
	}{
		args:=append([]string{"--environment",stateCLIEnv,"--incus-project","team"},tc.args...)
		var out,errOut bytes.Buffer
		code:=RunWithStateReaders(context.Background(),args,&out,&errOut,nil,nil,nil,fixture,fixture,fixture)
		if code!=0||errOut.Len()!=0||!strings.Contains(out.String(),tc.expected){t.Fatalf("args=%v code=%d out=%q err=%q",args,code,out.String(),errOut.String())}
		if strings.Contains(strings.Join(tc.args," "),"json"){
			var obj map[string]json.RawMessage
			if err:=json.Unmarshal(out.Bytes(),&obj);err!=nil||obj["kind"]==nil{t.Fatalf("invalid json: %s: %v",out.Bytes(),err)}
		}
	}
}
func TestStateCLIDeniesAbsentAuthorityAndProject(t *testing.T){
	for _,args:=range [][]string{
		{"--environment",stateCLIEnv,"--incus-project","team","state","show","-o","json"},
		{"--environment",stateCLIEnv,"--incus-project","team","state","diff","-o","yaml"},
		{"--environment",stateCLIEnv,"state","show","-o","json"},
		{"--environment",stateCLIEnv,"--incus-project","team","state","show","--layer","observed"},
	}{
		var out,errOut bytes.Buffer
		code:=Run(context.Background(),args,&out,&errOut,nil,nil)
		if code==0||out.Len()!=0{t.Fatalf("args=%v code=%d out=%q err=%q",args,code,out.String(),errOut.String())}
	}
}

func TestStateOperationsAllowsStructuredOutputGate(t *testing.T) {
	t.Setenv("AGINCTUS_RESTRICTED_READ_CONFIG", "")
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{
		"--environment", stateCLIEnv, "--incus-project", "team", "state", "operations",
		"--workload", "worker", "--revision", strings.Repeat("a", 64), "-o", "json",
	}, &out, &errOut, nil, nil)
	if code == 0 || strings.Contains(errOut.String(), "output format is not supported by this command") {
		t.Fatalf("state operations did not pass structured-output gate: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
}
