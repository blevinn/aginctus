package environment

import (
	"strings"
	"testing"

	state "github.com/blevinn/aginctus/internal/orchestration/state"
)

func TestProjectConfiguredWorkloadsCopiesAndPreservesOrder(t *testing.T){
	e:=Descriptor{ID:"11111111-1111-4111-8111-111111111111",Name:"dev",Binding:Binding{ServerID:"srv",Project:"aginctus"}}
	c:=Candidate{
		APIVersion:CandidateAPIVersion,Kind:CandidateKind,Environment:e,
		SelectedWorkloads:[]string{"a"},
		Workloads:[]state.WorkloadSpec{{ID:"a",InstanceName:"instance-a",InstanceType:"container",ImageFingerprint:strings.Repeat("a",64),Profiles:[]string{"base","overlay"},EnsureRunning:true}},
	}
	out,err:=ProjectConfiguredWorkloads(c,e,e.Binding)
	if err!=nil{t.Fatal(err)}
	if out.Kind!="ConfiguredWorkloadList"||out.Layer!="configured"||len(out.Items)!=1||out.Items[0].Profiles[0]!="base" {t.Fatalf("projection=%#v",out)}
	out.Items[0].Profiles[0]="changed"
	if c.Workloads[0].Profiles[0]!="base"{t.Fatal("projection aliases caller")}
	for _,tc:=range []struct{name string;enrolled Descriptor;binding Binding}{
		{"wrong project",e,Binding{ServerID:"srv",Project:"other"}},
		{"wrong server",e,Binding{ServerID:"alias",Project:"aginctus"}},
		{"wrong identity",Descriptor{ID:"22222222-2222-4222-8222-222222222222",Name:"dev",Binding:e.Binding},e.Binding},
	}{
		t.Run(tc.name,func(t *testing.T){
			result,err:=ProjectConfiguredWorkloads(c,tc.enrolled,tc.binding)
			if err==nil||len(result.Items)!=0{t.Fatalf("unsafe candidate projection=%#v err=%v",result,err)}
		})
	}
}
