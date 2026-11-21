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

// These golden documents are the experimental public wire contract for
// shipped v1alpha1 projections. They are not the internal Incus DTOs.
func TestV1Alpha1WireCompatibility(t *testing.T) {
	cases := []struct {
		name string
		value api.Document
	}{
		{
			name: "plugin-list",
			value: api.PluginList{TypeMeta: api.NewTypeMeta("PluginList"), Items: []api.Plugin{{
				Name: "example-agent", Version: "1.0", APIVersion: "aginctus.dev/plugin/v1alpha1",
				Capabilities: []api.PluginCapability{{Name: "assistant", Kind: "agent"}},
				Dependencies: []string{},
			}}},
		},
		{
			name: "scoped-workload-status-list",
			value: api.ScopedWorkloadStatusList{
				TypeMeta: api.NewTypeMeta("ScopedWorkloadStatusList"), Project: "aginctus",
				Items: []api.ScopedWorkloadStatus{{Name: "dev", Instance: "aginctus-dev", State: "Running"}},
			},
		},
		{
			name: "error",
			value: api.NewError(api.CodeNotFound, "workload not found"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture, err := os.ReadFile(filepath.Join("testdata",tc.name+".json"))
			if err != nil { t.Fatal(err) }
			var jsonOut, yamlOut bytes.Buffer
			if err := WriteStructured(&jsonOut, JSON, tc.value); err != nil { t.Fatal(err) }
			if !bytes.Equal(jsonOut.Bytes(), fixture) {
				t.Fatalf("breaking JSON wire change\nactual:\n%s\nexpected:\n%s", jsonOut.Bytes(), fixture)
			}
			if err := WriteStructured(&yamlOut, YAML, tc.value); err != nil { t.Fatal(err) }
			yamlJSON, err := yaml.YAMLToJSON(yamlOut.Bytes())
			if err != nil { t.Fatalf("YAML is invalid: %v",err) }
			var fromYAML, fromJSON any
			if err := json.Unmarshal(yamlJSON, &fromYAML); err != nil { t.Fatal(err) }
			if err := json.Unmarshal(jsonOut.Bytes(), &fromJSON); err != nil { t.Fatal(err) }
			if !reflect.DeepEqual(fromYAML, fromJSON) {
				t.Fatalf("YAML changed the JSON contract: yaml=%#v json=%#v", fromYAML, fromJSON)
			}
			if _, ok := fromJSON.(map[string]any)["apiVersion"]; !ok { t.Fatal("missing apiVersion") }
			if _, ok := fromJSON.(map[string]any)["kind"]; !ok { t.Fatal("missing kind") }
		})
	}
}

func TestEmptyCollectionsRemainArrays(t *testing.T) {
	for _, doc := range []api.Document{
		api.PluginList{TypeMeta: api.NewTypeMeta("PluginList"), Items: []api.Plugin{}},
		api.ScopedWorkloadStatusList{TypeMeta: api.NewTypeMeta("ScopedWorkloadStatusList"), Project: "aginctus", Items: []api.ScopedWorkloadStatus{}},
	} {
		var result bytes.Buffer
		if err := WriteStructured(&result, JSON, doc); err != nil { t.Fatal(err) }
		var object map[string]json.RawMessage
		if err := json.Unmarshal(result.Bytes(), &object); err != nil { t.Fatal(err) }
		if string(object["items"]) != "[]" { t.Fatalf("empty collection must encode [] not null: %s",result.Bytes()) }
	}
}
