package client

import (
	"context"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
)

func validLogicalWorkload() api.Workload {
	return api.Workload{
		Name:"gateway",EnvironmentID:fakeEnvID,Class:api.WorkloadClassGateway,
		Implementation:"litellm",Components:[]api.WorkloadComponent{
			{Name:"gateway",Instance:"container-a"},{Name:"db",Instance:"container-b"},
		},
	}
}

func TestLogicalWorkloadValidationRejectsContradictoryComponents(t *testing.T) {
	for _,change:=range []func(*api.Workload){
		func(w *api.Workload){w.EnvironmentID="foreign"},
		func(w *api.Workload){w.Name=""},
		func(w *api.Workload){w.Implementation=""},
		func(w *api.Workload){w.Class="unknown"},
		func(w *api.Workload){w.Components=nil},
		func(w *api.Workload){w.Components[1].Name="gateway"},
		func(w *api.Workload){w.Components[0].Name=""},
	} {
		w:=validLogicalWorkload()
		change(&w)
		if err:=validateLogicalWorkload(w,fakeEnvID);err==nil {t.Fatalf("invalid resource accepted: %+v",w)}
	}
	if err:=validateLogicalWorkload(validLogicalWorkload(),fakeEnvID);err!=nil {t.Fatal(err)}
}

func TestLogicalStatusValidationRejectsContradictoryObservations(t *testing.T) {
	base:=api.WorkloadStatus{TypeMeta:api.NewTypeMeta("WorkloadStatus"),
		EnvironmentID:fakeEnvID,Name:"gateway",Observed:[]api.ComponentObservation{
			{Name:"gateway",State:"Running"},{Name:"db",State:"Stopped"},
		}}
	if err:=validateLogicalObservations(base);err!=nil{t.Fatal(err)}
	for _,change:=range []func(*api.WorkloadStatus){
		func(s *api.WorkloadStatus){s.Observed=nil},
		func(s *api.WorkloadStatus){s.Observed[0].Name=""},
		func(s *api.WorkloadStatus){s.Observed[1].Name="gateway"},
		func(s *api.WorkloadStatus){s.Observed[0].State=""},
	} {
		status:=base
		status.Observed=append([]api.ComponentObservation(nil),base.Observed...)
		change(&status)
		if err:=validateLogicalObservations(status);err==nil{t.Fatalf("invalid observation accepted: %+v",status)}
	}
}

func TestLogicalWorkloadClientRejectsNonUUIDEnvironmentSelectorBeforeReader(t *testing.T) {
	reader:=&fakeLogicalReader{}
	service:=New(Options{LogicalWorkloadReader:reader}).Workloads
	got,err:=service.List(context.Background(),api.ListWorkloadsRequest{Project:"team",EnvironmentID:"dev"})
	if err==nil||got.Kind!=""||reader.calls!=0 {
		t.Fatalf("display name reached logical reader: result=%+v err=%v calls=%d",got,err,reader.calls)
	}
}
