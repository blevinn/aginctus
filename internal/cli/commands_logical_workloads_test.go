package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
	"github.com/blevinn/aginctus/pkg/client"
)
type testLogicalWorkloadReader struct {foreign bool}
func (r testLogicalWorkloadReader) ListWorkloads(_ context.Context,req api.ListWorkloadsRequest)(api.WorkloadList,error){
	env:=req.EnvironmentID;if r.foreign{env="foreign"}
	return api.WorkloadList{TypeMeta:api.NewTypeMeta("WorkloadList"),EnvironmentID:env,
		Items:[]api.Workload{{Name:"gateway",EnvironmentID:env,Class:api.WorkloadClassGateway,Implementation:"litellm",
			Components:[]api.WorkloadComponent{{Name:"main"},{Name:"database"}}}}},nil
}
func (r testLogicalWorkloadReader) GetWorkload(_ context.Context,req api.GetWorkloadRequest)(api.WorkloadDetail,error){
	env:=req.EnvironmentID;if r.foreign{env="foreign"}
	return api.WorkloadDetail{TypeMeta:api.NewTypeMeta("Workload"),Workload:api.Workload{Name:req.Name,EnvironmentID:env,Class:api.WorkloadClassGateway,Implementation:"litellm",Components:[]api.WorkloadComponent{{Name:"main"}}}},nil
}
func (r testLogicalWorkloadReader) WorkloadStatus(_ context.Context,req api.WorkloadStatusRequest)(api.WorkloadStatus,error){
	env:=req.EnvironmentID;if r.foreign{env="foreign"}
	return api.WorkloadStatus{TypeMeta:api.NewTypeMeta("WorkloadStatus"),EnvironmentID:env,Name:req.Name,Observed:[]api.ComponentObservation{{Name:"main",State:"Running"}}},nil
}
func TestInjectedLogicalWorkloadCLIOutput(t *testing.T){
	const env="11111111-1111-4111-8111-111111111111"
	for _,tc:=range []struct{args []string;want string}{
		{[]string{"--environment",env,"--incus-project","aginctus","workloads","list","-o","json"},`"kind": "WorkloadList"`},
		{[]string{"workloads","list","--environment",env,"--incus-project","aginctus","-o","wide"},"COMPONENTS"},
		{[]string{"--environment",env,"--incus-project","aginctus","workloads","show","gateway","-o","yaml"},"kind: Workload"},
		{[]string{"--environment",env,"--incus-project","aginctus","workloads","status","gateway"},"Running"},
	}{
		var out,errOut bytes.Buffer
		code:=RunWithLogicalWorkloads(context.Background(),tc.args,&out,&errOut,nil,nil,nil,testLogicalWorkloadReader{})
		if code!=0||errOut.Len()!=0||!strings.Contains(out.String(),tc.want){
			t.Fatalf("args=%v code=%d out=%q err=%q",tc.args,code,out.String(),errOut.String())
		}
		if strings.Contains(strings.Join(tc.args," "),"-o json"){
			var obj api.WorkloadList
			if err:=json.Unmarshal(out.Bytes(),&obj);err!=nil||obj.Items[0].EnvironmentID!=env {t.Fatalf("typed output=%#v err=%v",obj,err)}
		}
	}
}
func TestLogicalWorkloadCLIFailsClosed(t *testing.T){
	const env="11111111-1111-4111-8111-111111111111"
	for _,tc:=range []struct{args []string;reader client.LogicalWorkloadReader}{
		{[]string{"workloads","list","-o","json"},testLogicalWorkloadReader{}},
		{[]string{"workloads","list","--environment",env,"--incus-project","aginctus","-o","json"},nil},
		{[]string{"workloads","list","--environment",env,"--incus-project","aginctus","-o","json"},testLogicalWorkloadReader{foreign:true}},
		{[]string{"--environment",env,"plugins","list"},testLogicalWorkloadReader{}},
	}{
		var out,errOut bytes.Buffer
		code:=RunWithLogicalWorkloads(context.Background(),tc.args,&out,&errOut,nil,nil,nil,tc.reader)
		if code==0||out.Len()!=0 {t.Fatalf("args=%v code=%d out=%q err=%q",tc.args,code,out.String(),errOut.String())}
	}
}
