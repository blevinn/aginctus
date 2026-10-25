package workload

import (
	"encoding/json"
	"strings"
	"testing"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

func TestScopedExistingWorkloadPlanIsInstanceOnlyAndExplicitlyProjectBound(t *testing.T) {
	scope, err := incusscope.NewProjectScope("aginctus")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := testSpec().PlanScopedExisting(scope)
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].ID != "workload-dev" ||
		plan.Steps[0].Driver != "apply" {
		t.Fatalf("unexpected plan steps: %#v", plan.Steps)
	}
	if strings.Contains(string(plan.Steps[0].Configuration), "\"kind\":\"network\"") {
		t.Fatal("scoped instance plan includes a network resource")
	}
	var cfg struct {
		Project                  string            `json:"project"`
		Operation                string            `json:"operation"`
		EnsureRunning            bool              `json:"ensureRunning"`
		RejectUnsupportedChanges bool              `json:"rejectUnsupportedChanges"`
		RequireExistingConfig    map[string]string `json:"requireExistingConfig"`
		Documents                []json.RawMessage `json:"documents"`
	}
	if err := json.Unmarshal(plan.Steps[0].Configuration, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Project != "aginctus" || cfg.Operation != "upsert" ||
		!cfg.EnsureRunning || !cfg.RejectUnsupportedChanges || len(cfg.Documents) != 1 {
		t.Fatalf("unexpected scoped apply options: %#v", cfg)
	}
	for key, want := range map[string]string{
		"user.aginctus.managed": "true",
		"user.aginctus.resource": "workload",
		"user.aginctus.workload": "dev",
		"user.aginctus.role": "agent-workload",
	} {
		if cfg.RequireExistingConfig[key] != want {
			t.Fatalf("missing scoped ownership guard %s: %#v", key, cfg.RequireExistingConfig)
		}
	}
	var doc struct {
		Kind    string         `json:"kind"`
		Name    string         `json:"name"`
		Devices map[string]any `json:"devices"`
	}
	if err := json.Unmarshal(cfg.Documents[0], &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Kind != "instance" || doc.Name != "aginctus-dev" ||
		doc.Devices["management"] == nil {
		t.Fatalf("scoped workload lost declared instance/device configuration: %#v", doc)
	}
	// The original v0.1 plan still carries its host bridge step and has no
	// implicit Incus project selection.
	legacy, err := testSpec().Plan("upsert", false)
	if err != nil || len(legacy.Steps) != 2 {
		t.Fatalf("legacy plan changed: %#v %v", legacy, err)
	}
	var legacyCfg struct{ Project string `json:"project"` }
	if err := json.Unmarshal(legacy.Steps[1].Configuration, &legacyCfg); err != nil ||
		legacyCfg.Project != "" {
		t.Fatalf("legacy apply acquired implicit project: %q %v", legacyCfg.Project, err)
	}
}

func TestScopedExistingWorkloadPlanRejectsDefaultAndInvalidProjects(t *testing.T) {
	for _, project := range []string{"", "default", "../escape", "project/name", strings.Repeat("x", 101)} {
		plan, err := testSpec().PlanScopedExisting(incusscope.ProjectScope{Name: project})
		if err == nil || len(plan.Steps) != 0 {
			t.Fatalf("unsafe project %q accepted: %#v", project, plan)
		}
	}
}
