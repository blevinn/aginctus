package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type fakeDriver struct {
	name           string
	order          *[]string
	supportsDryRun bool
	validateErr    error
	executeErr     error
	executions     int
}

func (d *fakeDriver) Validate(json.RawMessage) error {
	return d.validateErr
}

func (d *fakeDriver) SupportsDryRun() bool {
	return d.supportsDryRun
}

func (d *fakeDriver) Execute(_ context.Context, _ json.RawMessage, options ExecuteOptions) error {
	d.executions++
	if d.order != nil {
		*d.order = append(*d.order, fmt.Sprintf("%s:%t", d.name, options.DryRun))
	}
	return d.executeErr
}

func testPlan(steps ...Step) Plan {
	return Plan{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "test"},
		Steps:      steps,
	}
}

func TestEngineExecutesStepsSequentially(t *testing.T) {
	var order []string
	apply := &fakeDriver{name: "apply", order: &order, supportsDryRun: true}
	compose := &fakeDriver{name: "compose", order: &order, supportsDryRun: true}
	engine := NewEngine(map[string]Driver{
		"apply":   apply,
		"compose": compose,
	})

	result, err := engine.Execute(context.Background(), testPlan(
		Step{ID: "one", Driver: "apply", Configuration: []byte(`{}`)},
		Step{ID: "two", Driver: "compose", Configuration: []byte(`{}`)},
	), ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if want := []string{"apply:false", "compose:false"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("execution order = %#v, want %#v", order, want)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("result steps = %#v", result.Steps)
	}
}

func TestEngineFailsFast(t *testing.T) {
	var order []string
	apply := &fakeDriver{name: "apply", order: &order, supportsDryRun: true, executeErr: errors.New("failed")}
	compose := &fakeDriver{name: "compose", order: &order, supportsDryRun: true}
	engine := NewEngine(map[string]Driver{
		"apply":   apply,
		"compose": compose,
	})

	_, err := engine.Execute(context.Background(), testPlan(
		Step{ID: "one", Driver: "apply", Configuration: []byte(`{}`)},
		Step{ID: "two", Driver: "compose", Configuration: []byte(`{}`)},
	), ExecuteOptions{})
	if err == nil || !strings.Contains(err.Error(), `step "one" (apply)`) {
		t.Fatalf("Execute() error = %v", err)
	}
	if want := []string{"apply:false"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("execution order = %#v, want %#v", order, want)
	}
	if compose.executions != 0 {
		t.Fatalf("compose executions = %d, want 0", compose.executions)
	}
}

func TestEngineValidatesEntirePlanBeforeMutation(t *testing.T) {
	apply := &fakeDriver{name: "apply", supportsDryRun: true}
	compose := &fakeDriver{name: "compose", supportsDryRun: true, validateErr: errors.New("invalid compose")}
	engine := NewEngine(map[string]Driver{
		"apply":   apply,
		"compose": compose,
	})

	_, err := engine.Execute(context.Background(), testPlan(
		Step{ID: "one", Driver: "apply", Configuration: []byte(`{}`)},
		Step{ID: "two", Driver: "compose", Configuration: []byte(`{}`)},
	), ExecuteOptions{})
	if err == nil {
		t.Fatal("Execute() error = nil, want validation error")
	}
	if apply.executions != 0 {
		t.Fatalf("apply executions = %d, want 0", apply.executions)
	}
}

func TestEngineRejectsUnknownDriver(t *testing.T) {
	engine := NewEngine(nil)
	_, err := engine.Execute(context.Background(), testPlan(
		Step{ID: "one", Driver: "missing", Configuration: []byte(`{}`)},
	), ExecuteOptions{})
	if err == nil || !strings.Contains(err.Error(), "unknown driver") {
		t.Fatalf("Execute() error = %v, want unknown driver error", err)
	}
}

func TestEngineEnforcesDryRunCapability(t *testing.T) {
	driver := &fakeDriver{name: "apply", supportsDryRun: false}
	engine := NewEngine(map[string]Driver{"apply": driver})

	_, err := engine.Execute(context.Background(), testPlan(
		Step{ID: "one", Driver: "apply", Configuration: []byte(`{}`)},
	), ExecuteOptions{DryRun: true})
	if err == nil || !strings.Contains(err.Error(), "does not support dry-run") {
		t.Fatalf("Execute() error = %v, want dry-run capability error", err)
	}
	if driver.executions != 0 {
		t.Fatalf("driver executions = %d, want 0", driver.executions)
	}
}
