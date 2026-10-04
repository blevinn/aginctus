package orchestration

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const (
	APIVersion = "aginctus.dev/v1alpha1"
	Kind       = "Orchestration"
)

type Metadata struct {
	Name string `json:"name"`
}

type Step struct {
	ID            string          `json:"id"`
	Driver        string          `json:"driver"`
	Configuration json.RawMessage `json:"configuration"`
}

type Plan struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Steps      []Step   `json:"steps"`
}

func ParsePlan(data []byte) (Plan, error) {
	var plan Plan
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return Plan{}, fmt.Errorf("decode orchestration plan: %w", err)
	}
	if decoder.More() {
		return Plan{}, fmt.Errorf("decode orchestration plan: unexpected trailing JSON")
	}
	if err := plan.Validate(); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func (p Plan) Validate() error {
	if p.APIVersion != APIVersion {
		return fmt.Errorf("orchestration apiVersion must be %q, got %q", APIVersion, p.APIVersion)
	}
	if p.Kind != Kind {
		return fmt.Errorf("orchestration kind must be %q, got %q", Kind, p.Kind)
	}
	if p.Metadata.Name == "" {
		return fmt.Errorf("orchestration metadata.name must not be empty")
	}
	if len(p.Steps) == 0 {
		return fmt.Errorf("orchestration must contain at least one step")
	}

	seen := make(map[string]struct{}, len(p.Steps))
	for i, step := range p.Steps {
		if step.ID == "" {
			return fmt.Errorf("orchestration step %d id must not be empty", i)
		}
		if _, ok := seen[step.ID]; ok {
			return fmt.Errorf("orchestration step id %q is duplicated", step.ID)
		}
		seen[step.ID] = struct{}{}
		if step.Driver == "" {
			return fmt.Errorf("orchestration step %q driver must not be empty", step.ID)
		}
		if len(bytes.TrimSpace(step.Configuration)) == 0 || bytes.Equal(bytes.TrimSpace(step.Configuration), []byte("null")) {
			return fmt.Errorf("orchestration step %q configuration must not be empty", step.ID)
		}
	}
	return nil
}
