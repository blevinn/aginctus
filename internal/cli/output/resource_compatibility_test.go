package output

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
	"sigs.k8s.io/yaml"
)

// Pin schemas for the merged environment and logical workload API. Fixture
// updates are public compatibility reviews, not snapshot regeneration.
func TestEnvironmentAndWorkloadWireContracts(t *testing.T) {
	const envID="11111111-1111-4111-8111-111111111111"
	cases:=[]struct{name string;value api.Document}{
		{"environment-list",api.EnvironmentList{
			TypeMeta:api.NewTypeMeta("EnvironmentList"),
			Items:[]api.Environment{{ID:envID,Name:"dev",Binding:api.EnvironmentBinding{ServerID:"srv-1",Project:"team"}}},
		}},
		{"workload-list",api.WorkloadList{
			TypeMeta:api.NewTypeMeta("WorkloadList"),EnvironmentID:envID,
			Items:[]api.Workload{{
				Name:"inference",EnvironmentID:envID,Class:api.WorkloadClassGateway,Implementation:"litellm",
				Components:[]api.WorkloadComponent{{Name:"gateway",Instance:"gateway-1"},{Name:"database",Instance:"postgres-1"}},
			}},
		}},
		{"configured-workload-list",api.ConfiguredWorkloadList{
			TypeMeta:api.NewTypeMeta("ConfiguredWorkloadList"),EnvironmentID:envID,Layer:"configured",
			Items:[]api.ConfiguredWorkload{{
				ID:"dev",InstanceName:"aginctus-dev",InstanceType:"container",ImageFingerprint:strings.Repeat("a",64),
				Profiles:[]string{"base","overlay"},EnsureRunning:true,
			}},
		}},
		{"workload-status",api.WorkloadStatus{
			TypeMeta:api.NewTypeMeta("WorkloadStatus"),EnvironmentID:envID,Name:"inference",
			AcceptedRevision:"rev-42",Observed:[]api.ComponentObservation{{Name:"gateway",State:"Running"},{Name:"database",State:"Running"}},
		}},
		{"plugin-detail",api.PluginDetail{
			TypeMeta:api.NewTypeMeta("Plugin"),
			Plugin:api.Plugin{Name:"example-agent",Version:"1.0",APIVersion:"aginctus.dev/plugin/v1alpha1",
				Capabilities:[]api.PluginCapability{{Name:"assistant",Kind:"agent"}},Dependencies:[]string{},
			},
		}},
	}
	for _,tc:=range cases{
		t.Run(tc.name,func(t *testing.T){
			want,err:=os.ReadFile(filepath.Join("testdata",tc.name+".json"));if err!=nil{t.Fatal(err)}
			var js,ys bytes.Buffer
			if err:=WriteStructured(&js,JSON,tc.value);err!=nil{t.Fatal(err)}
			if !bytes.Equal(js.Bytes(),want){t.Fatalf("wire contract changed\ngot: %s\nwant: %s",js.Bytes(),want)}
			if err:=WriteStructured(&ys,YAML,tc.value);err!=nil{t.Fatal(err)}
			fromYAML,err:=yaml.YAMLToJSON(ys.Bytes());if err!=nil{t.Fatal(err)}
			var jsonObject,yamlObject any
			if err:=json.Unmarshal(js.Bytes(),&jsonObject);err!=nil{t.Fatal(err)}
			if err:=json.Unmarshal(fromYAML,&yamlObject);err!=nil{t.Fatal(err)}
			if !reflect.DeepEqual(jsonObject,yamlObject){t.Fatalf("JSON/YAML contracts disagree: %s / %s",js.Bytes(),ys.Bytes())}
		})
	}
}
