package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	collection "github.com/blevinn/aginctus/internal/observability"
	report "github.com/blevinn/aginctus/pkg/observability"
	contract "github.com/blevinn/aginctus/pkg/orchestration"
)

func validateReport(t *testing.T, result Result, terminal report.State) []report.Event {
	t.Helper()
	if result.Report == nil || result.reporting != nil || len(result.Report.Events) == 0 {
		t.Fatalf("report missing or private collector escaped: %+v", result)
	}
	events := result.Report.Events
	for i, event := range events {
		if err := event.Validate(); err != nil || event.RunID != result.Report.RunID || (i > 0 && event.Sequence <= events[i-1].Sequence) {
			t.Fatalf("invalid ordered event: %+v error=%v", event, err)
		}
	}
	last := events[len(events)-1]
	if status := result.Report.Status; !status.Finished || status.Run != last || status.Watermark != last.Sequence || status.Dropped != result.Report.Dropped {
		t.Fatalf("authoritative terminal status differs: %+v", status)
	}
	if last.Kind != report.Run || last.State != terminal || last.Sequence != uint64(len(events))+result.Report.Dropped {
		t.Fatalf("terminal/gap mismatch: %+v dropped=%d", last, result.Report.Dropped)
	}
	return events
}

type inspectingDriver struct {
	t       *testing.T
	monitor *collection.Monitor
}

func (d inspectingDriver) ValidateContext(context.Context, json.RawMessage) error { return nil }
func (d inspectingDriver) SupportsDryRun() bool                                   { return false }
func (d inspectingDriver) Execute(_ context.Context, _ json.RawMessage, options ExecuteOptions) error {
	if options.Monitor != nil {
		d.t.Fatal("host monitor passed to driver")
	}
	s := d.monitor.Snapshot()
	if s.Finished || s.Run.State != report.Running || s.Entries[len(s.Entries)-1].Kind != report.Operation {
		d.t.Fatalf("live execution snapshot unavailable: %+v", s)
	}
	return nil
}

func TestMonitorExposesLiveExecutionAndReuseCannotMixRuns(t *testing.T) {
	engine, _, _, plan := gateEngine(nil, CheckPolicy{})
	monitor := collection.NewMonitor()
	engine.drivers[plan.Steps[0].Driver] = inspectingDriver{t, monitor}
	result, err := engine.Execute(t.Context(), plan, ExecuteOptions{Monitor: monitor})
	if err != nil {
		t.Fatal(err)
	}
	validateReport(t, result, report.Completed)
	if monitor.Snapshot().RunID != result.Report.RunID || !monitor.Snapshot().Finished {
		t.Fatal("monitor not bound to result")
	}
	// Reuse must not redirect the original reader or influence the next execution.
	engine, _, _, plan = gateEngine(nil, CheckPolicy{})
	next, err := engine.Execute(t.Context(), plan, ExecuteOptions{Monitor: monitor})
	if err != nil || next.Report.RunID == result.Report.RunID || monitor.Snapshot().RunID != result.Report.RunID {
		t.Fatalf("monitor reuse mixed independent runs: %v", err)
	}
}

func TestAbandonedObserverCannotBlockExecutionOrHideTerminalStatus(t *testing.T) {
	engine, _, _, plan := gateEngine(nil, CheckPolicy{})
	monitor := collection.NewMonitor()
	s, err := monitor.Subscribe(monitor.Snapshot().Cursor())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Enough independent operations to exceed both subscriber and trace bounds.
	first := plan.Steps[0]
	plan.Steps = nil
	for i := range collection.EventLimit {
		step := first
		step.ID = fmt.Sprintf("operation-%d", i)
		plan.Steps = append(plan.Steps, step)
	}
	result, err := engine.Execute(t.Context(), plan, ExecuteOptions{Monitor: monitor})
	if err != nil {
		t.Fatal(err)
	}
	validateReport(t, result, report.Completed)
	if !monitor.Snapshot().Finished || len(result.Operations) != len(plan.Steps) {
		t.Fatal("observer changed execution")
	}
	u, err := s.Next(t.Context())
	if err != nil || !u.Gap {
		t.Fatalf("slow observer was not disconnected with a gap: %+v %v", u, err)
	}
}

func TestReportingIsOptInAndRunLocal(t *testing.T) {
	engine, _, _, plan := gateEngine(nil, CheckPolicy{})
	plain, err := engine.Execute(t.Context(), plan, ExecuteOptions{})
	if err != nil || plain.Report != nil {
		t.Fatalf("legacy execution changed: %+v %v", plain, err)
	}
	first, err := engine.Execute(t.Context(), plan, ExecuteOptions{Reporting: true})
	if err != nil {
		t.Fatal(err)
	}
	events := validateReport(t, first, report.Completed)
	want := []report.Kind{report.Run, report.Validation, report.Validation, report.Authorization, report.Authorization,
		report.Preflight, report.Preflight, report.Step, report.Operation, report.Operation, report.Step,
		report.Step, report.Operation, report.Operation, report.Step, report.Run}
	var kinds []report.Kind
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}
	if !reflect.DeepEqual(kinds, want) || len(first.Steps) != len(plain.Steps) || len(first.Operations) != len(plain.Operations) {
		t.Fatalf("execution ordering changed: %v", kinds)
	}
	second, err := engine.Execute(t.Context(), plan, ExecuteOptions{Reporting: true})
	if err != nil {
		t.Fatal(err)
	}
	validateReport(t, second, report.Completed)
	if first.Report.RunID == second.Report.RunID || second.Report.Events[0].Sequence != 1 {
		t.Fatal("independent runs share correlation/sequence")
	}
}

func TestReadinessEventsCorrelateAttemptsAndGateStepCompletion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		check := readinessCheck(true)
		check.AcceptedRevision = "accepted-1"
		calls := 0
		check.Evaluate = func(context.Context) (contract.Finding, error) {
			calls++
			outcome := contract.Unknown
			if calls == 2 {
				outcome = contract.Passed
			}
			return contract.Finding{TargetID: gateScope.Target, CheckID: "ready", Outcome: outcome,
				ObservedAt: time.Now(), Code: "fixture", AcceptedRevision: "accepted-1"}, nil
		}
		engine, _, second, plan := gateEngine(map[string]StepConditions{"first": {Scope: gateScope, Postconditions: []BoundCheck{check}}}, CheckPolicy{})
		result, err := engine.Execute(t.Context(), plan, ExecuteOptions{Reporting: true})
		if err != nil || second.executions != 1 {
			t.Fatalf("ready operation blocked: %v", err)
		}
		events := validateReport(t, result, report.Completed)
		var states []report.State
		var attempts []int
		passed, dependent := -1, -1
		for i, event := range events {
			if event.Kind == report.Condition || event.Kind == report.CheckAttempt {
				states, attempts = append(states, event.State), append(attempts, event.Attempt)
				if event.StepID != "step-1" || event.CheckID != "ready" || event.Scope.Instance != "one" || event.Phase != "postcondition" || !event.Required || event.AcceptedRevision != "accepted-1" {
					t.Fatalf("check correlation lost: %+v", event)
				}
				if event.Kind == report.Condition && event.State == report.Passed {
					passed = i
				}
			}
			if event.Kind == report.Operation && event.State == report.Running && event.StepID == "step-2" {
				dependent = i
			}
		}
		if !reflect.DeepEqual(states, []report.State{report.Running, report.Running, report.Unknown, report.Waiting, report.Running, report.Passed, report.Passed}) ||
			!reflect.DeepEqual(attempts, []int{0, 1, 1, 1, 2, 2, 0}) || passed < 0 || dependent <= passed {
			t.Fatalf("attempt/lifecycle order wrong: states=%v attempts=%v passed=%d dependent=%d", states, attempts, passed, dependent)
		}
	})
}

func TestReportingPreservesBlockedPartialResultsWithoutLeakingPayloads(t *testing.T) {
	secret := "DO_NOT_EMIT_THIS_SECRET"
	check := gateCheck("ready", true, contract.Failed)
	evaluate := check.Evaluate
	check.Evaluate = func(ctx context.Context) (contract.Finding, error) {
		finding, _ := evaluate(ctx)
		finding.Code, finding.Message, finding.Remediation = secret, secret, secret
		return finding, nil
	}
	engine, _, second, plan := gateEngine(map[string]StepConditions{secret: {Scope: gateScope, Postconditions: []BoundCheck{check}}}, CheckPolicy{})
	plan.Steps[0].ID, plan.Steps[0].Configuration, plan.Metadata.Name = secret, []byte(`{"token":"`+secret+`"}`), secret
	result, err := engine.Execute(t.Context(), plan, ExecuteOptions{Reporting: true})
	if !errors.Is(err, ErrConditionNotMet) || second.executions != 0 || len(result.Operations) != 1 || len(result.Steps) != 0 {
		t.Fatalf("postflight lost partial result: %+v error=%v", result, err)
	}
	events := validateReport(t, result, report.Blocked)
	encoded, err := json.Marshal(result.Report)
	if err != nil || strings.Contains(string(encoded), secret) {
		t.Fatalf("trace copied arbitrary input/finding text: %s error=%v", encoded, err)
	}
	if events[len(events)-2].Kind != report.Step || events[len(events)-2].State != report.Blocked {
		t.Fatal("step success was emitted after failed postflight")
	}
}

func TestReportingTerminalFailuresAndDryRunAreHonest(t *testing.T) {
	for _, name := range []string{"validation", "preflight", "operation", "canceled", "dry-run"} {
		t.Run(name, func(t *testing.T) {
			engine, first, _, plan := gateEngine(nil, CheckPolicy{})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			options := ExecuteOptions{Reporting: true}
			want := report.Failed
			switch name {
			case "validation":
				first.validateErr = errors.New("SECRET driver error")
			case "preflight":
				check := gateCheck("ready", true, contract.Unknown)
				check.Preflight = true
				engine = engine.WithConditions(map[string]StepConditions{"second": {Scope: gateScope, Preconditions: []BoundCheck{check}}}, CheckPolicy{})
				want = report.Blocked
			case "operation":
				first.executeErr = errors.New("SECRET driver error")
				want = report.Uncertain
			case "canceled":
				cancel()
				want = report.Canceled
			case "dry-run":
				for _, driver := range engine.drivers {
					driver.(*fakeDriver).supportsDryRun = true
				}
				engine = NewEngine(engine.drivers)
				options.DryRun, want = true, report.Simulated
			}
			result, err := engine.Execute(ctx, plan, options)
			if (err == nil) != (name == "dry-run") {
				t.Fatalf("unexpected execution error: %v", err)
			}
			events := validateReport(t, result, want)
			for _, event := range events {
				if event.Kind == report.Operation && event.State == report.Completed {
					t.Fatalf("failed/simulated operation claimed completion: %+v", event)
				}
			}
			encoded, _ := json.Marshal(result.Report)
			if strings.Contains(string(encoded), "SECRET") {
				t.Fatal("raw error reached event payload")
			}
		})
	}
}

type cancelingReportDriver struct{ cancel context.CancelFunc }

func (cancelingReportDriver) ValidateContext(context.Context, json.RawMessage) error { return nil }
func (cancelingReportDriver) SupportsDryRun() bool                                   { return false }
func (d cancelingReportDriver) Execute(context.Context, json.RawMessage, ExecuteOptions) error {
	d.cancel()
	return nil
}

func TestCancellationAfterDriverEntryIsReportedAsUncertain(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	engine := NewEngine(map[string]Driver{"cancel": cancelingReportDriver{cancel}})
	result, err := engine.Execute(ctx, testPlan(Step{ID: "selected", Driver: "cancel", Configuration: []byte(`{}`)}), ExecuteOptions{Reporting: true})
	if !errors.Is(err, context.Canceled) || len(result.Operations) != 0 || len(result.Steps) != 0 {
		t.Fatalf("canceled driver claimed success: %+v error=%v", result, err)
	}
	events := validateReport(t, result, report.Uncertain)
	if events[len(events)-3].Kind != report.Operation || events[len(events)-3].State != report.Uncertain || events[len(events)-1].Reason != report.Cancellation {
		t.Fatal("uncertain operation/cancellation reason lost")
	}
}

func TestOptionalConditionFailureDoesNotFailReportedRun(t *testing.T) {
	check := gateCheck("diagnostic", false, contract.Failed)
	engine, _, second, plan := gateEngine(map[string]StepConditions{"first": {Scope: gateScope, Postconditions: []BoundCheck{check}}}, CheckPolicy{})
	result, err := engine.Execute(t.Context(), plan, ExecuteOptions{Reporting: true})
	if err != nil || second.executions != 1 {
		t.Fatalf("optional finding blocked: %v", err)
	}
	events := validateReport(t, result, report.Completed)
	found := false
	for _, event := range events {
		if event.Kind == report.Condition && event.State == report.Failed && !event.Required {
			found = true
		}
	}
	if !found {
		t.Fatal("optional diagnostic missing from trace")
	}
}

func TestReadinessWaitTimeoutAndCancellationDoNotInventAttempts(t *testing.T) {
	for _, cancelParent := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			check := readinessCheck(true)
			check.Evaluate = gateCheck("ready", true, contract.Unknown).Evaluate
			want, attempts := report.Blocked, 5
			if cancelParent {
				time.AfterFunc(500*time.Millisecond, cancel)
				want, attempts = report.Canceled, 1
			}
			engine, _, second, plan := gateEngine(map[string]StepConditions{"first": {Scope: gateScope, Postconditions: []BoundCheck{check}}}, CheckPolicy{})
			result, err := engine.Execute(ctx, plan, ExecuteOptions{Reporting: true})
			if err == nil || second.executions != 0 {
				t.Fatal("wait released a blocked dependent")
			}
			events := validateReport(t, result, want)
			started := 0
			var last report.Event
			for _, event := range events {
				if event.Kind == report.CheckAttempt && event.State == report.Running {
					started++
				}
				if event.Kind == report.Condition {
					last = event
				}
			}
			if started != attempts || last.Attempt != 0 || last.ObservedAt != result.Conditions[len(result.Conditions)-1].Finding.ObservedAt || last.Reason == report.NoReason {
				t.Fatalf("wait invented observation or lost reason: started=%d last=%+v", started, last)
			}
		})
	}
}

func TestTraceOverflowDoesNotChangeExecutionOrLoseTerminal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		check := readinessCheck(true)
		check.Check.Timeout, check.Check.RetryInterval = time.Second, time.Millisecond
		check.Evaluate = gateCheck("ready", true, contract.Unknown).Evaluate
		engine, _, second, plan := gateEngine(map[string]StepConditions{"first": {Scope: gateScope, Postconditions: []BoundCheck{check}}}, CheckPolicy{})
		result, err := engine.Execute(t.Context(), plan, ExecuteOptions{Reporting: true})
		if !errors.Is(err, ErrConditionNotMet) || second.executions != 0 || len(result.Conditions) != 1000 {
			t.Fatalf("retention changed evaluation: attempts=%d error=%v", len(result.Conditions), err)
		}
		validateReport(t, result, report.Blocked)
		if len(result.Report.Events) != collection.EventLimit || result.Report.Dropped == 0 {
			t.Fatal("overflow was unbounded or unreported")
		}
	})
}
