package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLogicalWorkloadSchemaKeepsComponentsAndLayersSeparate(t *testing.T) {
	resource:=WorkloadDetail{TypeMeta:NewTypeMeta("Workload"),Workload:Workload{
		Name:"inference",EnvironmentID:"11111111-1111-4111-8111-111111111111",
		Class:WorkloadClassGateway,Implementation:"litellm",
		Components:[]WorkloadComponent{{Name:"gateway",Instance:"incus-gateway"},{Name:"database",Instance:"incus-postgres"}},
	}}
	var _ Document = resource
	data,err:=json.Marshal(resource)
	if err!=nil {t.Fatal(err)}
	if !strings.Contains(string(data),`"class":"gateway"`) || !strings.Contains(string(data),`"name":"database"`) || strings.Contains(string(data),"acceptedRevision") {
		t.Fatalf("invalid resource schema: %s",data)
	}
	status:=WorkloadStatus{TypeMeta:NewTypeMeta("WorkloadStatus"),EnvironmentID:resource.EnvironmentID,Name:resource.Name,Observed:[]ComponentObservation{}}
	data,err=json.Marshal(status)
	if err!=nil {t.Fatal(err)}
	if !strings.Contains(string(data),`"observed":[]`) || strings.Contains(string(data),"acceptedRevision") {
		t.Fatalf("invalid observation schema: %s",data)
	}
	list:=WorkloadList{TypeMeta:NewTypeMeta("WorkloadList"),EnvironmentID:resource.EnvironmentID,Items:[]Workload{}}
	data,err=json.Marshal(list)
	if err!=nil {t.Fatal(err)}
	if !strings.Contains(string(data),`"items":[]`) {t.Fatal(string(data))}
}
