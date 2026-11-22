package native

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
)

func acceptedAssessmentFixture(t *testing.T) (environment.Candidate,environment.AcceptedRevision,[]AcceptedWorkloadObservation) {
	t.Helper()
	c:=environment.Candidate{
		APIVersion:environment.CandidateAPIVersion,Kind:environment.CandidateKind,
		Environment:environment.Descriptor{
			ID:"7c82e6d1-1212-4123-8123-0123456789ab",Name:"development",
			Binding:environment.Binding{ServerID:"server-id",Project:"team"},
		},
		SelectedWorkloads:[]string{"worker"},
		Workloads:[]acceptedstate.WorkloadSpec{{
			ID:"worker",InstanceName:"worker-1",InstanceType:"container",
			ImageFingerprint:strings.Repeat("a",64),Profiles:[]string{"default"},
			EnsureRunning:true,
		}},
	}
	rev,err:=environment.BuildAcceptedRevision(nil,nil,c)
	if err!=nil{t.Fatal(err)}
	obs:=[]AcceptedWorkloadObservation{{
		WorkloadID:"worker",Instance:&InstanceObservation{
			Identity:InstanceIdentity{Project:"team",Name:"worker-1"},
			Type:"container",State:"Running",Profiles:[]string{"default"},
			ImageFingerprint:strings.Repeat("a",64),
			Config:map[string]string{
				"volatile.base_image":strings.Repeat("a",64),
				"user.aginctus.managed":"true",
				"user.aginctus.resource":"workload",
				"user.aginctus.role":"agent-workload",
				"user.aginctus.workload":"worker",
				"user.aginctus.environment":c.Environment.ID,
			},
		},
	}}
	return c,rev,obs
}

func TestPinnedAcceptedAssessmentConvergenceAndFieldOnlyDrift(t *testing.T) {
	c,rev,obs:=acceptedAssessmentFixture(t)
	got,err:=AssessPinnedAcceptedWorkloads(c.Environment,rev,c,obs)
	if err!=nil || got.RevisionID!=rev.ID || len(got.Workloads)!=1 ||
		got.Workloads[0].Status!="converged" || len(got.Workloads[0].Fields)!=0 {
		t.Fatalf("convergence: %+v %v",got,err)
	}
	obs[0].Instance.State="Stopped"
	got,err=AssessPinnedAcceptedWorkloads(c.Environment,rev,c,obs)
	if err!=nil || got.Workloads[0].Status!="drift" ||
		len(got.Workloads[0].Fields)!=1 || got.Workloads[0].Fields[0]!="ensureRunning" {
		t.Fatalf("drift: %+v %v",got,err)
	}
	payload,err:=json.Marshal(got)
	if err!=nil{t.Fatal(err)}
	if strings.Contains(string(payload),strings.Repeat("a",64)) {
		t.Fatal("assessment disclosed image fingerprint instead of a field name")
	}
	obs[0].Instance=nil
	obs[0].Absent=true
	got,err=AssessPinnedAcceptedWorkloads(c.Environment,rev,c,obs)
	if err!=nil || got.Workloads[0].Status!="absent" {
		t.Fatalf("explicit absence: %+v %v",got,err)
	}
}

func TestPinnedAcceptedAssessmentRejectsStaleForeignOrAmbiguousReads(t *testing.T) {
	for _,tc:=range []struct{name string; modify func(*environment.Candidate,*environment.AcceptedRevision,*[]AcceptedWorkloadObservation)}{
		{"foreign environment",func(c *environment.Candidate,_ *environment.AcceptedRevision,_ *[]AcceptedWorkloadObservation){c.Environment.ID="7c82e6d1-1212-4123-8123-0123456789ac"}},
		{"stale revision",func(_ *environment.Candidate,r *environment.AcceptedRevision,_ *[]AcceptedWorkloadObservation){r.CandidateDigest=strings.Repeat("f",64)}},
		{"missing observation",func(_ *environment.Candidate,_ *environment.AcceptedRevision,o *[]AcceptedWorkloadObservation){*o=nil}},
		{"ambiguous absence",func(_ *environment.Candidate,_ *environment.AcceptedRevision,o *[]AcceptedWorkloadObservation){(*o)[0].Absent=true}},
		{"foreign project",func(_ *environment.Candidate,_ *environment.AcceptedRevision,o *[]AcceptedWorkloadObservation){(*o)[0].Instance.Identity.Project="foreign"}},
		{"foreign environment label",func(_ *environment.Candidate,_ *environment.AcceptedRevision,o *[]AcceptedWorkloadObservation){(*o)[0].Instance.Config["user.aginctus.environment"]="foreign"}},
		{"wrong instance",func(_ *environment.Candidate,_ *environment.AcceptedRevision,o *[]AcceptedWorkloadObservation){(*o)[0].Instance.Identity.Name="foreign"}},
		{"no owned marker",func(_ *environment.Candidate,_ *environment.AcceptedRevision,o *[]AcceptedWorkloadObservation){delete((*o)[0].Instance.Config,"user.aginctus.managed")}},
		{"unknown lifecycle",func(_ *environment.Candidate,_ *environment.AcceptedRevision,o *[]AcceptedWorkloadObservation){(*o)[0].Instance.State="Unknown"}},
		{"unexpected observed entry",func(_ *environment.Candidate,_ *environment.AcceptedRevision,o *[]AcceptedWorkloadObservation){*o=append(*o,AcceptedWorkloadObservation{WorkloadID:"foreign",Absent:true})}},
	} {
		t.Run(tc.name,func(t *testing.T){
			c,r,o:=acceptedAssessmentFixture(t)
			tc.modify(&c,&r,&o)
			if _,err:=AssessPinnedAcceptedWorkloads(
				environment.Descriptor{
					ID:"7c82e6d1-1212-4123-8123-0123456789ab",Name:"development",
					Binding:environment.Binding{ServerID:"server-id",Project:"team"},
				},r,c,o);err==nil {
				t.Fatal("accepted stale, ambiguous or foreign observation")
			}
		})
	}
}
