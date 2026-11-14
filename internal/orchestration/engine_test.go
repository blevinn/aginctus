package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeDriver struct {
	name           string
	order          *[]string
	supportsDryRun bool
	validateErr    error
	validate       func(context.Context) error
	validations    int
	executeErr     error
	executions     int
}

func (d *fakeDriver) ValidateContext(ctx context.Context, _ json.RawMessage) error {
	d.validations++
	if d.validate != nil {
		return d.validate(ctx)
	}
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

func TestEngineRejectsCancellationBeforeFirstStep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	driver := &fakeDriver{name: "apply", supportsDryRun: true}
	engine := NewEngine(map[string]Driver{"apply": driver})
	_, err := engine.Execute(ctx, testPlan(Step{ID: "first", Driver: "apply", Configuration: []byte(`{}`)}), ExecuteOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if driver.validations != 0 || driver.executions != 0 {
		t.Fatal("driver validated or ran after cancellation")
	}
}

func TestEngineCompletesPreflightBeforeExecution(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry-run=%t", dryRun), func(t *testing.T) {
			var order []string
			ctx := t.Context()
			drivers := map[string]Driver{}
			for _, name := range []string{"first", "second"} {
				drivers[name] = &fakeDriver{
					name: name, order: &order, supportsDryRun: true,
					validate: func(got context.Context) error {
						if got != ctx {
							t.Error("preflight did not receive caller context")
						}
						order = append(order, "validate-"+name)
						return nil
					},
				}
			}
			result, err := NewEngine(drivers).Execute(ctx, testPlan(
				Step{ID: "first", Driver: "first", Configuration: []byte(`{}`)},
				Step{ID: "second", Driver: "second", Configuration: []byte(`{}`)},
			), ExecuteOptions{DryRun: dryRun})
			if err != nil || len(result.Steps) != 2 {
				t.Fatalf("result=%#v error=%v", result, err)
			}
			want := []string{"validate-first", "validate-second", fmt.Sprintf("first:%t", dryRun), fmt.Sprintf("second:%t", dryRun)}
			if !reflect.DeepEqual(order, want) {
				t.Fatalf("order=%v want=%v", order, want)
			}
		})
	}
}

func TestEngineRejectsCancellationAfterValidationCallback(t *testing.T) {
	for _, callbackErr := range []error{nil, errors.New("preflight failed")} {
		t.Run(fmt.Sprint(callbackErr), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			first := &fakeDriver{validate: func(context.Context) error {
				cancel()
				return callbackErr
			}}
			second := &fakeDriver{}
			result, err := NewEngine(map[string]Driver{"first": first, "second": second}).Execute(ctx, testPlan(
				Step{ID: "first", Driver: "first", Configuration: []byte(`{}`)},
				Step{ID: "second", Driver: "second", Configuration: []byte(`{}`)},
			), ExecuteOptions{})
			if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), `step "first" (first)`) {
				t.Fatalf("error=%v, want scoped cancellation", err)
			}
			if callbackErr != nil && !errors.Is(err, callbackErr) {
				t.Fatalf("validation cause lost: %v", err)
			}
			if len(result.Steps) != 0 || first.executions != 0 || second.executions != 0 || second.validations != 0 {
				t.Fatalf("work continued after canceled preflight: result=%#v first=%#v second=%#v", result, first, second)
			}
		})
	}
}

func TestEngineCancelsBlockedPreflight(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	driver := &fakeDriver{validate: func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}
	engine := NewEngine(map[string]Driver{"blocking": driver})
	finished := make(chan error, 1)
	go func() {
		_, err := engine.Execute(ctx, testPlan(Step{ID: "probe", Driver: "blocking", Configuration: []byte(`{}`)}), ExecuteOptions{})
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("preflight did not start")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) || driver.executions != 0 {
			t.Fatalf("error=%v executions=%d", err, driver.executions)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("preflight did not exit after cancellation")
	}
}

func TestEngineValidateContextRejectsExpiredDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	driver := &fakeDriver{}
	err := NewEngine(map[string]Driver{"test": driver}).ValidateContext(ctx,
		testPlan(Step{ID: "test", Driver: "test", Configuration: []byte(`{}`)}), ExecuteOptions{})
	if !errors.Is(err, context.DeadlineExceeded) || driver.validations != 0 {
		t.Fatalf("error=%v validations=%d", err, driver.validations)
	}
}

type cancelingDriver struct {
	fakeDriver
	cancel context.CancelFunc
}

func (d *cancelingDriver) Execute(ctx context.Context, raw json.RawMessage, opts ExecuteOptions) error {
	d.cancel()
	return nil
}

func TestEngineDoesNotStartLaterStepAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	first := &cancelingDriver{cancel: cancel}
	second := &fakeDriver{name: "second", supportsDryRun: true}
	engine := NewEngine(map[string]Driver{"first": first, "second": second})
	result, err := engine.Execute(ctx, testPlan(
		Step{ID: "first", Driver: "first", Configuration: []byte(`{}`)},
		Step{ID: "second", Driver: "second", Configuration: []byte(`{}`)},
	), ExecuteOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if len(result.Steps) != 0 || second.executions != 0 {
		t.Fatalf("second executed: %#v", result)
	}
}

func TestEngineDoesNotReportCanceledStepAsSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := &cancelingDriver{cancel: cancel}
	second := &fakeDriver{name: "second", supportsDryRun: true}
	engine := NewEngine(map[string]Driver{"first": first, "second": second})
	result, err := engine.Execute(ctx, testPlan(
		Step{ID: "first", Driver: "first", Configuration: []byte(`{}`)},
		Step{ID: "second", Driver: "second", Configuration: []byte(`{}`)},
	), ExecuteOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if len(result.Steps) != 0 || second.executions != 0 {
		t.Fatalf("canceled step reported success or next step ran: %#v, calls=%d", result, second.executions)
	}
}
