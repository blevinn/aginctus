package mcpschema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The validation messages are part of the operator-facing contract. A
// golden fixture pins them so a wording change is reviewed rather than
// discovered in a log.
func TestValidationGoldenCases(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "validation-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name   string         `json:"name"`
			Config map[string]any `json:"config"`
			Error  string         `json:"error"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("the validation golden fixture is empty")
	}
	for _, testCase := range fixture.Cases {
		t.Run(testCase.Name, func(t *testing.T) {
			config, err := Parse(testCase.Config)
			if err == nil {
				_, err = config.Resolve()
			}
			if err == nil {
				t.Fatal("the configuration was accepted")
			}
			if err.Error() != testCase.Error {
				t.Fatalf("error = %q, want %q", err.Error(), testCase.Error)
			}
		})
	}
}
