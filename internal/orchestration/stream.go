package orchestration

import (
	"bytes"
	"fmt"
	"io"

	yaml "gopkg.in/yaml.v3"
	sigyaml "sigs.k8s.io/yaml"
)

// ParsePlanStream accepts one or more YAML/JSON orchestration documents.
// Each document contains a complete Orchestration, and steps execute in
// document order. Validate the combined plan before executing any step.
func ParsePlanStream(data []byte) (Plan, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var combined Plan
	count := 0
	for {
		var node yaml.Node
		err := decoder.Decode(&node)
		if err == io.EOF {
			break
		}
		if err != nil {
			return Plan{}, fmt.Errorf("decode orchestration document %d: %w", count+1, err)
		}
		if len(node.Content) == 0 {
			return Plan{}, fmt.Errorf("empty orchestration document %d", count+1)
		}
		encoded, err := yaml.Marshal(&node)
		if err != nil {
			return Plan{}, fmt.Errorf("encode orchestration document %d: %w", count+1, err)
		}
		doc, err := sigyaml.YAMLToJSONStrict(encoded)
		if err != nil {
			return Plan{}, fmt.Errorf("convert orchestration document %d: %w", count+1, err)
		}
		plan, err := ParsePlan(doc)
		if err != nil {
			return Plan{}, fmt.Errorf("orchestration document %d: %w", count+1, err)
		}
		if count == 0 {
			combined = plan
		} else {
			if combined.APIVersion != plan.APIVersion || combined.Kind != plan.Kind || combined.Metadata.Name != plan.Metadata.Name {
				return Plan{}, fmt.Errorf("orchestration document %d metadata differs from first document", count+1)
			}
			combined.Steps = append(combined.Steps, plan.Steps...)
		}
		count++
	}
	if count == 0 {
		return Plan{}, fmt.Errorf("orchestration stream is empty")
	}
	if err := combined.Validate(); err != nil {
		return Plan{}, err
	}
	return combined, nil
}
