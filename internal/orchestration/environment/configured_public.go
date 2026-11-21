package environment

import (
	"fmt"
	"slices"

	"github.com/blevinn/aginctus/pkg/api"
)

// ProjectConfiguredWorkloads provides a versioned, read-only projection of a
// *selected configured candidate*. This is not an enrolled workload catalog
// or accepted/observed read. The caller must independently authenticate the
// supplied binding; source text and environment flags are not proof.
func ProjectConfiguredWorkloads(candidate Candidate,enrolled Descriptor,verified Binding)(api.ConfiguredWorkloadList,error){
	selected,err:=SelectCandidate(candidate,enrolled,verified)
	if err!=nil {return api.ConfiguredWorkloadList{},fmt.Errorf("cannot project unverified candidate: %w",err)}
	out:=api.ConfiguredWorkloadList{
		TypeMeta:api.NewTypeMeta("ConfiguredWorkloadList"),
		EnvironmentID:enrolled.ID,Layer:"configured",Items:make([]api.ConfiguredWorkload,0,len(selected.Workloads)),
	}
	for _,w:=range selected.Workloads {
		out.Items=append(out.Items,api.ConfiguredWorkload{
			ID:w.ID,InstanceName:w.InstanceName,InstanceType:w.InstanceType,
			ImageFingerprint:w.ImageFingerprint,Profiles:slices.Clone(w.Profiles),
			EnsureRunning:w.EnsureRunning,
		})
	}
	slices.SortFunc(out.Items,func(a,b api.ConfiguredWorkload)int{
		if a.ID<b.ID{return -1};if a.ID>b.ID{return 1};return 0
	})
	return out,nil
}
