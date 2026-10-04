package orchestration

import (
	"strings"
	"testing"
)

func TestPlanValidateRejectsDuplicateStepIDs(t *testing.T) {
	plan := Plan{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "test"},
		Steps: []Step{
			{ID: "same", Driver: "one", Configuration: []byte(`{}`)},
			{ID: "same", Driver: "two", Configuration: []byte(`{}`)},
		},
	}

	err := plan.Validate()
	if err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("Validate() error = %v, want duplicate step error", err)
	}
}

func TestParsePlanRejectsUnknownFields(t *testing.T) {
	_, err := ParsePlan([]byte(`{
		"apiVersion":"aginctus.dev/v1alpha1",
		"kind":"Orchestration",
		"metadata":{"name":"test"},
		"steps":[{"id":"one","driver":"apply","configuration":{},"unexpected":true}]
	}`))
	if err == nil {
		t.Fatal("ParsePlan() error = nil, want error")
	}
}
