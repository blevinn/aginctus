package native

import (
	"errors"
	"slices"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
)

// AcceptedWorkloadObservation is an EXPLICIT, completed read result for one
// selected workload. Absent means the observer authoritatively determined
// absence in the selected project. A timeout/permission error must NOT be
// represented as Absent. A present workload requires its full observation.
type AcceptedWorkloadObservation struct {
	WorkloadID string
	Absent bool
	Instance *InstanceObservation
}

// AcceptedWorkloadAssessment is a nonsecret, READ-ONLY snapshot comparison.
// It is not an executable native plan and carries no write authority.
type AcceptedWorkloadAssessment struct {
	WorkloadID string   `json:"workloadId"`
	InstanceName string `json:"instanceName"`
	Status string `json:"status"`
	Fields []string `json:"fields"`
}

type AcceptedEnvironmentAssessment struct {
	EnvironmentID string `json:"environmentId"`
	ServerID string `json:"serverId"`
	Project string `json:"project"`
	RevisionID string `json:"revisionId"`
	Generation uint64 `json:"generation"`
	Workloads []AcceptedWorkloadAssessment `json:"workloads"`
}

// AssessPinnedAcceptedWorkloads compares a fully verified immutable accepted
// candidate with a complete explicit set of observed instances. It cannot
// access Incus, create, adopt, remove, stop, or start an instance.
//
// The caller MUST obtain the revision from authenticated project state and
// observations from the same independently authenticated Incus project.
// These values do not themselves prove caller authority, enrollment, or
// observational freshness. A real writer needs a separate authorized fence,
// ETags, in-flight revocation checks, and new resource observations.
func AssessPinnedAcceptedWorkloads(
	enrolled environment.Descriptor,
	revision environment.AcceptedRevision,
	accepted environment.Candidate,
	observations []AcceptedWorkloadObservation,
) (AcceptedEnvironmentAssessment,error) {
	if enrolled.Validate()!=nil || !accepted.Environment.SameEnvironment(enrolled) ||
		revision.ValidateAgainst(accepted)!=nil {
		return AcceptedEnvironmentAssessment{},errors.New("verified accepted environment revision required")
	}
	if len(observations)!=len(accepted.Workloads) {
		return AcceptedEnvironmentAssessment{},errors.New("complete selected workload observations required")
	}
	byID:=make(map[string]AcceptedWorkloadObservation,len(observations))
	for _,o:=range observations {
		if o.WorkloadID=="" || (o.Absent && o.Instance!=nil) ||
			(!o.Absent && o.Instance==nil) {
			return AcceptedEnvironmentAssessment{},errors.New("invalid or ambiguous workload observation")
		}
		if _,exists:=byID[o.WorkloadID];exists {
			return AcceptedEnvironmentAssessment{},errors.New("duplicate workload observation")
		}
		byID[o.WorkloadID]=o
	}
	out:=AcceptedEnvironmentAssessment{
		EnvironmentID:enrolled.ID,ServerID:enrolled.Binding.ServerID,
		Project:enrolled.Binding.Project,RevisionID:revision.ID,
		Generation:revision.Generation,
		Workloads:make([]AcceptedWorkloadAssessment,0,len(accepted.Workloads)),
	}
	for _,w:=range accepted.Workloads {
		observed,ok:=byID[w.ID]
		if !ok {return AcceptedEnvironmentAssessment{},errors.New("missing selected workload observation")}
		delete(byID,w.ID)
		item:=AcceptedWorkloadAssessment{
			WorkloadID:w.ID,InstanceName:w.InstanceName,Fields:[]string{},
		}
		if observed.Absent {
			item.Status="absent"
			out.Workloads=append(out.Workloads,item)
			continue
		}
		instance:=observed.Instance
		if instance.Identity.Project!=enrolled.Binding.Project ||
			instance.Identity.Name!=w.InstanceName ||
			instance.Config["user.aginctus.managed"]!="true" ||
			instance.Config["user.aginctus.resource"]!="workload" ||
			instance.Config["user.aginctus.workload"]!=w.ID ||
			instance.Config["user.aginctus.environment"]!=enrolled.ID ||
			instance.Config["user.aginctus.role"]!="agent-workload" {
			return AcceptedEnvironmentAssessment{},errors.New("foreign or unowned observed environment workload")
		}
		if instance.State!="Running" && instance.State!="Stopped" {
			return AcceptedEnvironmentAssessment{},errors.New("unverified observed workload lifecycle")
		}
		if instance.Type!=w.InstanceType {item.Fields=append(item.Fields,"instanceType")}
		if instance.ImageFingerprint!=w.ImageFingerprint ||
			instance.Config["volatile.base_image"]!=w.ImageFingerprint {
			item.Fields=append(item.Fields,"imageFingerprint")
		}
		if !slices.Equal(instance.Profiles,w.Profiles) {item.Fields=append(item.Fields,"profiles")}
		if w.EnsureRunning && instance.State!="Running" {
			item.Fields=append(item.Fields,"ensureRunning")
		}
		if len(item.Fields)>0 {item.Status="drift"} else {item.Status="converged"}
		out.Workloads=append(out.Workloads,item)
	}
	if len(byID)!=0 {return AcceptedEnvironmentAssessment{},errors.New("observation included unselected workload")}
	slices.SortFunc(out.Workloads,func(a,b AcceptedWorkloadAssessment)int {
		if a.WorkloadID<b.WorkloadID{return -1}
		if a.WorkloadID>b.WorkloadID{return 1}
		return 0
	})
	return out,nil
}
