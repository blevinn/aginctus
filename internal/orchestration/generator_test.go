package orchestration

import (
	"reflect"
	"strings"
	"testing"
)

func TestGeneratorUsesEmbeddedLibraryDeterministically(t *testing.T) {
	generator, err := NewGenerator()
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}

	source := `
		local ag = import 'aginctus/orchestration.libsonnet';
		ag.orchestration('test', [
			ag.apply('network', {
				name: std.extVar('config').network,
			}),
		])
	`

	inputs := map[string]any{
		"network": "aginctus-mgmt",
	}

	first, err := generator.Evaluate(source, inputs)
	if err != nil {
		t.Fatalf("Evaluate() first error = %v", err)
	}
	second, err := generator.Evaluate(source, inputs)
	if err != nil {
		t.Fatalf("Evaluate() second error = %v", err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("generated plans differ:\nfirst: %#v\nsecond: %#v", first, second)
	}
	if first.Metadata.Name != "test" || len(first.Steps) != 1 || first.Steps[0].Driver != "apply" {
		t.Fatalf("generated plan = %#v", first)
	}
}

func TestGeneratorRejectsFilesystemImport(t *testing.T) {
	generator, err := NewGenerator()
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}

	_, err = generator.Evaluate(`import '/etc/passwd'`, nil)
	if err == nil {
		t.Fatal("Evaluate() error = nil, want import error")
	}
	if !strings.Contains(err.Error(), "/etc/passwd") {
		t.Fatalf("Evaluate() error = %v, want rejected path", err)
	}
}

func TestGeneratorAllowsExplicitRegisteredResource(t *testing.T) {
	generator, err := NewGenerator()
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}
	generator, err = generator.WithResource("local/workload.libsonnet", `{ step: { id: 'workload', driver: 'apply', configuration: {} } }`)
	if err != nil {
		t.Fatalf("WithResource() error = %v", err)
	}

	source := `
		local ag = import 'aginctus/orchestration.libsonnet';
		local workload = import 'local/workload.libsonnet';
		ag.orchestration('test', [workload.step])
	`
	plan, err := generator.Evaluate(source, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].ID != "workload" {
		t.Fatalf("generated plan = %#v", plan)
	}
}
