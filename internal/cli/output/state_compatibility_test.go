package output

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
	"sigs.k8s.io/yaml"
)

// These fixtures make the explicit accepted/configured layer direction and
// revision identity part of the reviewed v1alpha1 wire compatibility surface.
func TestStateReadWireGoldensAndYAMLParity(t *testing.T) {
	env := api.Environment{ID:"11111111-1111-4111-8111-111111111111",Name:"dev",
		Binding:api.EnvironmentBinding{ServerID:"server-1",Project:"team"}}
	revision := &api.AcceptedRevisionIdentity{
		ID:"revision-2",Generation:2,ParentID:"revision-1",CandidateDigest:"candidate-digest",
	}
	for _,tc:=range []struct{name string;doc api.Document}{
		{"accepted-state-status",api.AcceptedStateStatus{
			TypeMeta:api.NewTypeMeta("AcceptedStateStatus"),Environment:env,Layer:"accepted",
			HasAccepted:true,Revision:revision,
		}},
		{"configured-accepted-diff",api.ConfiguredAcceptedDiff{
			TypeMeta:api.NewTypeMeta("ConfiguredAcceptedDiff"),Environment:env,FromLayer:"accepted",
			ToLayer:"configured",HasAccepted:true,Revision:revision,ProposedDigest:"candidate-next",
			Changed:true,DisplayNameChanged:false,NewlySelected:[]string{"new-agent"},
			Unselected:[]string{},Modified:[]api.AcceptedWorkloadChange{
				{ID:"gateway",Fields:[]string{"imageFingerprint"}},
			},
		}},
	}{
		t.Run(tc.name,func(t *testing.T){
			want,err:=os.ReadFile(filepath.Join("testdata",tc.name+".json"))
			if err!=nil{t.Fatal(err)}
			var js,ys bytes.Buffer
			if err:=WriteStructured(&js,JSON,tc.doc);err!=nil{t.Fatal(err)}
			if !bytes.Equal(js.Bytes(),want){
				t.Fatalf("public wire contract changed\nwant:\n%s\ngot:\n%s",want,js.Bytes())
			}
			if err:=WriteStructured(&ys,YAML,tc.doc);err!=nil{t.Fatal(err)}
			yamlJSON,err:=yaml.YAMLToJSON(ys.Bytes())
			if err!=nil{t.Fatal(err)}
			var actual,converted any
			if err:=json.Unmarshal(js.Bytes(),&actual);err!=nil{t.Fatal(err)}
			if err:=json.Unmarshal(yamlJSON,&converted);err!=nil{t.Fatal(err)}
			if !reflect.DeepEqual(actual,converted){t.Fatalf("JSON/YAML disagree: %s / %s",js.Bytes(),ys.Bytes())}
		})
	}
}
