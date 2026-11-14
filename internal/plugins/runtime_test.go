package plugins_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	collection "github.com/blevinn/aginctus/internal/observability"
	engine "github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/plugins"
	report "github.com/blevinn/aginctus/pkg/observability"
	contract "github.com/blevinn/aginctus/pkg/orchestration"
	"github.com/blevinn/aginctus/pkg/plugin"
)

type runtimePlugin struct {
	contribution
	evaluate func(context.Context, plugin.CheckRequest, plugin.CheckServices) (contract.Finding, error)
}

func (p runtimePlugin) EvaluateCheck(ctx context.Context, request plugin.CheckRequest, services plugin.CheckServices) (contract.Finding, error) {
	return p.evaluate(ctx, request, services)
}

func lifecycleFinding(_ context.Context, request plugin.CheckRequest, services plugin.CheckServices) (contract.Finding, error) {
	o, err := services.Containers.Inspect("guest")
	outcome := contract.Unknown
	if o.State == contract.ContainerRunning {
		outcome = contract.Passed
	}
	return contract.Finding{TargetID: request.Target.ID, CheckID: request.Check.ID, Outcome: outcome,
		ObservedAt: o.ObservedAt, ResourceIncarnation: o.ResourceIncarnation, Code: "lifecycle"}, err
}

func runtimeFixture(t *testing.T, evaluate func(context.Context, plugin.CheckRequest, plugin.CheckServices) (contract.Finding, error)) (*plugins.Registry, plugins.ScopedTarget) {
	t.Helper()
	definition := declarations("images:fixture")[0]
	definition.DependsOn, definition.Observations = nil, nil
	definition.Postconditions[0] = contract.Check{ID: "ready", Claim: "container running", Scope: "selected container lifecycle", Required: true,
		Effect: contract.ReadOnly, Access: []contract.Access{contract.IncusAccess}, Timeout: time.Second}
	p := runtimePlugin{contribution: contribution{discovery: newDiscovery("example"), targets: func(context.Context, json.RawMessage) ([]contract.Target, error) {
		return []contract.Target{definition}, nil
	}}, evaluate: evaluate}
	r := registry(t, p)
	graph, err := r.Assemble(t.Context(), "lab", []plugins.Instance{instance("one", "example")})
	if err != nil {
		t.Fatal(err)
	}
	return r, graph.Targets[0]
}

func runningObservation() contract.ContainerObservation {
	return contract.ContainerObservation{State: contract.ContainerRunning, ObservedAt: time.Now(), ResourceIncarnation: "current"}
}

func TestRuntimeServicesAreScopedDetachedAndInvocationLocal(t *testing.T) {
	var retained plugin.ContainerInspector
	r, target := runtimeFixture(t, func(ctx context.Context, request plugin.CheckRequest, services plugin.CheckServices) (contract.Finding, error) {
		if request.Target.ID != "service" || request.Target.Containers[0].ID != "guest" || request.Check.Access[0] != contract.IncusAccess {
			t.Fatal("binding or earlier invocation mutated request")
		}
		retained = services.Containers
		request.Target.Containers[0].ID = "foreign"
		request.Check.Access[0] = contract.GuestAccess
		return lifecycleFinding(ctx, request, services)
	})
	calls := 0
	key := target.Key
	bound, err := r.BindCheck(target, "ready", func(ctx context.Context, got plugins.TargetKey, id string) (contract.ContainerObservation, error) {
		calls++
		if got != key || id != "guest" || ctx.Err() != nil {
			t.Fatalf("incorrect host selection: %s/%s", got, id)
		}
		return runningObservation(), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	target.Definition.Containers[0].ID = "changed"
	target.Definition.Postconditions[0].Access[0] = contract.NetworkAccess
	bound.Check.Access[0] = contract.HostAccess
	for range 2 {
		if finding, err := bound.Evaluate(t.Context()); err != nil || finding.Outcome != contract.Passed {
			t.Fatalf("scoped evaluation: %+v error=%v", finding, err)
		}
		if _, err := retained.Inspect("guest"); !errors.Is(err, context.Canceled) {
			t.Fatalf("retained service outlived invocation: %v", err)
		}
	}
	if calls != 2 {
		t.Fatalf("retained reader reached host: %d calls", calls)
	}
}

func TestIgnoredServiceFailuresCannotBecomePassedEvidence(t *testing.T) {
	denied := errors.New("host read denied")
	for _, name := range []string{"foreign container", "denied", "invalid", "stale", "future", "canceled"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			r, target := runtimeFixture(t, func(_ context.Context, request plugin.CheckRequest, services plugin.CheckServices) (contract.Finding, error) {
				id := "guest"
				if name == "foreign container" {
					id = "other/guest"
				}
				_, _ = services.Containers.Inspect(id)
				return contract.Finding{TargetID: request.Target.ID, CheckID: request.Check.ID, Outcome: contract.Passed, ObservedAt: time.Now(), Code: "ignored-error"}, nil
			})
			calls := 0
			bound, err := r.BindCheck(target, "ready", func(context.Context, plugins.TargetKey, string) (contract.ContainerObservation, error) {
				calls++
				o := runningObservation()
				switch name {
				case "denied":
					return o, denied
				case "invalid":
					o.ResourceIncarnation = "unsafe\nincarnation"
				case "stale":
					o.ObservedAt = o.ObservedAt.Add(-time.Hour)
				case "future":
					o.ObservedAt = o.ObservedAt.Add(time.Hour)
				case "canceled":
					cancel()
				}
				return o, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			plan := engine.Plan{APIVersion: engine.APIVersion, Kind: engine.Kind, Metadata: engine.Metadata{Name: "runtime"}, Steps: []engine.Step{{ID: "selected", Driver: "fake", Configuration: []byte(`"selected"`)}}}
			host := &conditionHost{completed: make(map[string]bool)}
			policy := engine.CheckPolicy{Mode: engine.ReadOnlyChecks, Authorize: func(context.Context, engine.TargetScope, contract.Check) error { return nil }}
			result, err := engine.NewEngine(map[string]engine.Driver{"fake": host}).WithConditions(map[string]engine.StepConditions{
				"selected": {Scope: engine.TargetScope{Deployment: "lab", Instance: "one", Target: "service"}, Postconditions: []engine.BoundCheck{bound}},
			}, policy).Execute(ctx, plan, engine.ExecuteOptions{})
			if err == nil || len(result.Steps) != 0 || len(result.Operations) != 1 || len(result.Conditions) != 1 || result.Conditions[0].Finding.Outcome != contract.Unknown {
				t.Fatalf("ignored error established success: result=%+v error=%v", result, err)
			}
			if name == "denied" && !errors.Is(err, denied) {
				t.Fatal("host denial cause lost")
			}
			if name == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation cause lost")
			}
			if name == "foreign container" && calls != 0 {
				t.Fatal("out-of-target read reached host")
			}
		})
	}
}

func TestRuntimeBindingRejectsUnavailableCapabilities(t *testing.T) {
	for _, name := range []string{"unknown check", "missing reader", "unsupported access", "unknown plugin", "invalid scope", "unadvertised capability", "missing evaluator"} {
		t.Run(name, func(t *testing.T) {
			r, target := runtimeFixture(t, lifecycleFinding)
			id := "ready"
			var inspect plugins.ContainerInspection = func(context.Context, plugins.TargetKey, string) (contract.ContainerObservation, error) {
				t.Fatal("binding performed I/O")
				return contract.ContainerObservation{}, nil
			}
			switch name {
			case "unknown check":
				id = "absent"
			case "missing reader":
				inspect = nil
			case "unsupported access":
				target.Definition.Postconditions[0].Access = []contract.Access{contract.GuestAccess}
			case "unknown plugin":
				target.Plugin = "absent"
			case "invalid scope":
				target.Key.Instance = ""
			case "unadvertised capability":
				target.Definition.Capability = "absent"
			case "missing evaluator":
				r = registry(t, newDiscovery("example"))
			}
			if _, err := r.BindCheck(target, id, inspect); err == nil {
				t.Fatal("invalid runtime binding accepted")
			}
		})
	}
}

func TestOfflineRuntimeChecksReceiveNoInspectionCapability(t *testing.T) {
	r, target := runtimeFixture(t, func(_ context.Context, request plugin.CheckRequest, services plugin.CheckServices) (contract.Finding, error) {
		if services.Containers != nil {
			t.Fatal("offline evaluator received external capability")
		}
		return contract.Finding{TargetID: request.Target.ID, CheckID: request.Check.ID, Outcome: contract.Passed, ObservedAt: time.Now(), Code: "offline"}, nil
	})
	target.Definition.Postconditions[0].Effect, target.Definition.Postconditions[0].Access = contract.Offline, nil
	bound, err := r.BindCheck(target, "ready", func(context.Context, plugins.TargetKey, string) (contract.ContainerObservation, error) {
		t.Fatal("offline evaluation performed a read")
		return contract.ContainerObservation{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bound.Evaluate(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeReaderCannotOutliveBoundDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, target := runtimeFixture(t, lifecycleFinding)
		bound, err := r.BindCheck(target, "ready", func(ctx context.Context, _ plugins.TargetKey, _ string) (contract.ContainerObservation, error) {
			<-ctx.Done()
			return runningObservation(), nil
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		started := time.Now()
		if _, err := bound.Evaluate(ctx); !errors.Is(err, context.DeadlineExceeded) || time.Since(started) != time.Second {
			t.Fatalf("reader lost deadline: elapsed=%v error=%v", time.Since(started), err)
		}
	})
}

func TestRuntimeReadersKeepIdenticalLocalIDsInSeparateInstances(t *testing.T) {
	r, _ := runtimeFixture(t, lifecycleFinding)
	graph, err := r.Assemble(t.Context(), "lab", []plugins.Instance{instance("one", "example"), instance("two", "example")})
	if err != nil {
		t.Fatal(err)
	}
	denied := errors.New("second instance not authorized")
	var selected []plugins.TargetKey
	inspect := func(_ context.Context, key plugins.TargetKey, id string) (contract.ContainerObservation, error) {
		selected = append(selected, key)
		if id != "guest" {
			t.Fatal("unexpected local container")
		}
		if key.Instance == "two" {
			return contract.ContainerObservation{}, denied
		}
		return runningObservation(), nil
	}
	for _, target := range graph.Targets {
		bound, err := r.BindCheck(target, "ready", inspect)
		if err != nil {
			t.Fatal(err)
		}
		finding, err := bound.Evaluate(t.Context())
		if target.Key.Instance == "one" {
			if err != nil || finding.Outcome != contract.Passed {
				t.Fatalf("first instance failed: %+v %v", finding, err)
			}
		} else if !errors.Is(err, denied) {
			t.Fatalf("first instance authorized second: %v", err)
		}
	}
	if len(selected) != 2 || selected[0].Instance != "one" || selected[1].Instance != "two" {
		t.Fatalf("logical scopes collapsed: %v", selected)
	}
}

func TestScopedRuntimeChecksUseEngineReadinessWindow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, target := runtimeFixture(t, lifecycleFinding)
		target.Definition.Postconditions[0].Timeout, target.Definition.Postconditions[0].RetryInterval = 5*time.Second, time.Second
		calls := 0
		bound, err := r.BindCheck(target, "ready", func(ctx context.Context, key plugins.TargetKey, id string) (contract.ContainerObservation, error) {
			calls++
			if key != target.Key || id != "guest" {
				t.Fatal("host received incorrect scope")
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("host read lost readiness deadline")
			}
			o := runningObservation()
			if calls == 1 {
				o.State = contract.ContainerStopped
			}
			return o, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		bound.ResourceIncarnation = "current"
		host := &conditionHost{completed: make(map[string]bool)}
		plan := engine.Plan{APIVersion: engine.APIVersion, Kind: engine.Kind, Metadata: engine.Metadata{Name: "runtime"}, Steps: []engine.Step{
			{ID: "provider", Driver: "fake", Configuration: []byte(`"provider"`)}, {ID: "consumer", Driver: "fake", Configuration: []byte(`"consumer"`)},
		}}
		policy := engine.CheckPolicy{Mode: engine.ReadOnlyChecks, Authorize: func(context.Context, engine.TargetScope, contract.Check) error { return nil }}
		result, err := engine.NewEngine(map[string]engine.Driver{"fake": host}).WithConditions(map[string]engine.StepConditions{
			"provider": {Scope: engine.TargetScope{Deployment: "lab", Instance: "one", Target: "service"}, Postconditions: []engine.BoundCheck{bound}},
		}, policy).Execute(t.Context(), plan, engine.ExecuteOptions{})
		if err != nil || calls != 2 || len(result.Conditions) != 2 || result.Conditions[1].Attempt != 2 || len(result.Steps) != 2 || !host.completed["consumer"] {
			t.Fatalf("runtime readiness lost: result=%+v calls=%d error=%v", result, calls, err)
		}
	})
}

func runProgressFixture(t *testing.T, ctx context.Context, evaluate func(context.Context, plugin.CheckRequest, plugin.CheckServices) (contract.Finding, error), enabled bool) (engine.Result, error) {
	t.Helper()
	r, target := runtimeFixture(t, evaluate)
	bound, err := r.BindCheck(target, "ready", func(context.Context, plugins.TargetKey, string) (contract.ContainerObservation, error) {
		return runningObservation(), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	host := &conditionHost{completed: make(map[string]bool)}
	plan := engine.Plan{APIVersion: engine.APIVersion, Kind: engine.Kind, Metadata: engine.Metadata{Name: "progress"},
		Steps: []engine.Step{{ID: "selected", Driver: "fake", Configuration: []byte(`"selected"`)}}}
	policy := engine.CheckPolicy{Mode: engine.ReadOnlyChecks, Authorize: func(context.Context, engine.TargetScope, contract.Check) error { return nil }}
	return engine.NewEngine(map[string]engine.Driver{"fake": host}).WithConditions(map[string]engine.StepConditions{
		"selected": {Scope: engine.TargetScope{Deployment: "lab", Instance: "one", Target: "service"}, Postconditions: []engine.BoundCheck{bound}},
	}, policy).Execute(ctx, plan, engine.ExecuteOptions{Reporting: enabled})
}

func progressEvents(t *testing.T, result engine.Result) []report.Event {
	t.Helper()
	if result.Report == nil {
		t.Fatal("missing run report")
	}
	var progress []report.Event
	var active report.Event
	var sequence uint64
	for _, event := range result.Report.Events {
		if event.Validate() != nil || event.Sequence <= sequence || event.RunID != result.Report.RunID {
			t.Fatalf("invalid ordered event: %+v", event)
		}
		sequence = event.Sequence
		if event.Kind == report.CheckAttempt {
			active = report.Event{}
			if event.State == report.Running {
				active = event
			}
		}
		if event.Kind == report.DomainProgress {
			if active.Kind == "" || event.StepID != active.StepID || event.Scope != active.Scope || event.Phase != active.Phase || event.CheckID != active.CheckID || event.Attempt != active.Attempt {
				t.Fatalf("progress escaped its core-owned attempt boundaries: %+v", event)
			}
			progress = append(progress, event)
		}
	}
	if sequence != uint64(len(result.Report.Events))+result.Report.Dropped {
		t.Fatal("progress gaps were hidden")
	}
	return progress
}

func TestScopedProgressHasHostCorrelationAndCannotPassACheck(t *testing.T) {
	invalid := []report.Progress{{}, {Code: "raw\nSECRET"}, {Code: strings.Repeat("x", 129)}, {Code: "work", Current: 1}, {Code: "work", Current: 2, Total: 1}}
	result, err := runProgressFixture(t, t.Context(), func(ctx context.Context, request plugin.CheckRequest, services plugin.CheckServices) (contract.Finding, error) {
		if services.Progress == nil {
			t.Fatal("enabled callback received no reporter")
		}
		for _, payload := range invalid {
			if payload.Validate() == nil {
				t.Fatalf("invalid public progress accepted: %+v", payload)
			}
			services.Progress.Report(payload)
		}
		services.Progress.Report(report.Progress{Code: "work", Current: 1, Total: 1})
		finding, err := lifecycleFinding(ctx, request, services)
		finding.Outcome = contract.Failed
		return finding, err
	}, true)
	if !errors.Is(err, engine.ErrConditionNotMet) || len(result.Operations) != 1 || len(result.Steps) != 0 {
		t.Fatalf("100%% progress established success: result=%+v error=%v", result, err)
	}
	progress := progressEvents(t, result)
	if len(progress) != 1 || result.Report.Dropped != uint64(len(invalid)) {
		t.Fatalf("invalid progress retained or gaps lost: %+v", result.Report)
	}
	event := progress[0]
	if event.State != report.Running || event.StepID != "step-1" || event.OperationID != "step-1" || event.Scope != (report.Scope{Deployment: "lab", Instance: "one", Target: "service"}) || event.Phase != "postcondition" || event.CheckID != "ready" || event.Attempt != 1 || !event.Required || !event.ObservedAt.IsZero() {
		t.Fatalf("host correlation/evidence boundary lost: %+v", event)
	}
	for _, modify := range []func(*report.Event){
		func(e *report.Event) { e.State = report.Completed },
		func(e *report.Event) { e.Attempt = 0 },
		func(e *report.Event) { e.ObservedAt = time.Now() },
		func(e *report.Event) { e.Reason = report.ConditionGate },
	} {
		bad := event
		modify(&bad)
		if bad.Validate() == nil {
			t.Fatalf("progress claimed lifecycle/evidence: %+v", bad)
		}
	}
	encoded, _ := json.Marshal(result.Report)
	if strings.Contains(string(encoded), "SECRET") || result.Report.Events[len(result.Report.Events)-1].State != report.Blocked {
		t.Fatal("invalid text escaped or terminal gate changed")
	}
}

func TestProgressIsAbsentWhenDisabledAndMasksInheritedBindings(t *testing.T) {
	result, err := runProgressFixture(t, t.Context(), func(ctx context.Context, request plugin.CheckRequest, services plugin.CheckServices) (contract.Finding, error) {
		if services.Progress != nil {
			t.Fatal("default execution received progress reporter")
		}
		return lifecycleFinding(ctx, request, services)
	}, false)
	if err != nil || result.Report != nil {
		t.Fatalf("default execution changed: %+v %v", result, err)
	}
	result, err = runProgressFixture(t, t.Context(), func(ctx context.Context, request plugin.CheckRequest, services plugin.CheckServices) (contract.Finding, error) {
		inner, err := runProgressFixture(t, ctx, func(ctx context.Context, request plugin.CheckRequest, services plugin.CheckServices) (contract.Finding, error) {
			if services.Progress != nil {
				t.Fatal("disabled nested run inherited outer correlation")
			}
			return lifecycleFinding(ctx, request, services)
		}, false)
		if err != nil || inner.Report != nil {
			t.Fatalf("disabled nested run changed: %+v %v", inner, err)
		}
		services.Progress.Report(report.Progress{Code: "outer"})
		return lifecycleFinding(ctx, request, services)
	}, true)
	if err != nil || len(progressEvents(t, result)) != 1 {
		t.Fatalf("nested reporting boundary failed: %v", err)
	}
}

func TestProgressLifetimeExpiresBeforeNextAttempt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var retained report.Reporter
		calls := 0
		r, target := runtimeFixture(t, func(ctx context.Context, request plugin.CheckRequest, services plugin.CheckServices) (contract.Finding, error) {
			calls++
			if retained != nil {
				retained.Report(report.Progress{Code: "expired"})
			}
			retained = services.Progress
			retained.Report(report.Progress{Code: "current"})
			return lifecycleFinding(ctx, request, services)
		})
		target.Definition.Postconditions[0].Timeout, target.Definition.Postconditions[0].RetryInterval = 5*time.Second, time.Second
		bound, err := r.BindCheck(target, "ready", func(context.Context, plugins.TargetKey, string) (contract.ContainerObservation, error) {
			o := runningObservation()
			if calls == 1 {
				o.State = contract.ContainerStopped
			}
			return o, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		host := &conditionHost{completed: make(map[string]bool)}
		plan := engine.Plan{APIVersion: engine.APIVersion, Kind: engine.Kind, Metadata: engine.Metadata{Name: "progress"}, Steps: []engine.Step{{ID: "selected", Driver: "fake", Configuration: []byte(`"selected"`)}}}
		policy := engine.CheckPolicy{Mode: engine.ReadOnlyChecks, Authorize: func(context.Context, engine.TargetScope, contract.Check) error { return nil }}
		result, err := engine.NewEngine(map[string]engine.Driver{"fake": host}).WithConditions(map[string]engine.StepConditions{
			"selected": {Scope: engine.TargetScope{Deployment: "lab", Instance: "one", Target: "service"}, Postconditions: []engine.BoundCheck{bound}},
		}, policy).Execute(t.Context(), plan, engine.ExecuteOptions{Reporting: true})
		if err != nil || calls != 2 || result.Report.Dropped != 0 {
			t.Fatalf("readiness reporting failed: %+v %v", result, err)
		}
		progress := progressEvents(t, result)
		if len(progress) != 2 || progress[0].Attempt != 1 || progress[1].Attempt != 2 || progress[0].Progress.Code != "current" || progress[1].Progress.Code != "current" {
			t.Fatalf("retained reporter escaped attempt lifetime: %+v", progress)
		}
		retained.Report(report.Progress{Code: "after-return"})
	})
}

func TestCanceledAndElapsedDeadlineProgressIsDiscarded(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			result, err := runProgressFixture(t, ctx, func(_ context.Context, request plugin.CheckRequest, services plugin.CheckServices) (contract.Finding, error) {
				if deadline {
					time.Sleep(time.Second) // exact deadline, including timer-delivery gaps
				} else {
					cancel()
				}
				services.Progress.Report(report.Progress{Code: "late"})
				return contract.Finding{TargetID: request.Target.ID, CheckID: request.Check.ID, Outcome: contract.Passed, ObservedAt: time.Now(), Code: "late"}, nil
			}, true)
			if err == nil || len(progressEvents(t, result)) != 0 || result.Report.Dropped != 0 || len(result.Steps) != 0 {
				t.Fatalf("expired progress accepted or convergence changed: %+v %v", result, err)
			}
		})
	}
}

func TestConcurrentProgressSharesSequenceAndOverflowKeepsTerminal(t *testing.T) {
	for _, count := range []int{128, collection.EventLimit + 10} {
		result, err := runProgressFixture(t, t.Context(), func(ctx context.Context, request plugin.CheckRequest, services plugin.CheckServices) (contract.Finding, error) {
			var workers sync.WaitGroup
			for range 8 {
				workers.Go(func() {
					for range count / 8 {
						services.Progress.Report(report.Progress{Code: "parallel"})
					}
				})
			}
			workers.Wait()
			return lifecycleFinding(ctx, request, services)
		}, true)
		if err != nil || len(result.Steps) != 1 || len(result.Operations) != 1 {
			t.Fatalf("progress changed execution: %+v %v", result, err)
		}
		progress := progressEvents(t, result)
		if count == 128 && (len(progress) != 128 || result.Report.Dropped != 0) {
			t.Fatal("concurrent progress was lost or misordered")
		}
		if count > collection.EventLimit && (len(result.Report.Events) != collection.EventLimit || result.Report.Dropped == 0) {
			t.Fatal("progress overflow unbounded or unreported")
		}
		last := result.Report.Events[len(result.Report.Events)-1]
		if last.Kind != report.Run || last.State != report.Completed {
			t.Fatal("progress displaced authoritative terminal result")
		}
	}
}

func TestProgressKeepsIdenticalLocalChecksInSeparateInstanceScopes(t *testing.T) {
	r, _ := runtimeFixture(t, func(ctx context.Context, request plugin.CheckRequest, services plugin.CheckServices) (contract.Finding, error) {
		services.Progress.Report(report.Progress{Code: "inspect"})
		return lifecycleFinding(ctx, request, services)
	})
	graph, err := r.Assemble(t.Context(), "lab", []plugins.Instance{instance("two", "example"), instance("one", "example")})
	if err != nil {
		t.Fatal(err)
	}
	plan := engine.Plan{APIVersion: engine.APIVersion, Kind: engine.Kind, Metadata: engine.Metadata{Name: "scoped-progress"}}
	bindings := make(map[string]engine.StepConditions)
	for _, target := range graph.Targets {
		bound, err := r.BindCheck(target, "ready", func(context.Context, plugins.TargetKey, string) (contract.ContainerObservation, error) {
			return runningObservation(), nil
		})
		if err != nil {
			t.Fatal(err)
		}
		id := target.Key.String()
		raw, _ := json.Marshal(id)
		plan.Steps = append(plan.Steps, engine.Step{ID: id, Driver: "fake", Configuration: raw})
		bindings[id] = engine.StepConditions{Scope: engine.TargetScope{Deployment: "lab", Instance: target.Key.Instance, Target: "service"}, Postconditions: []engine.BoundCheck{bound}}
	}
	host := &conditionHost{completed: make(map[string]bool)}
	policy := engine.CheckPolicy{Mode: engine.ReadOnlyChecks, Authorize: func(context.Context, engine.TargetScope, contract.Check) error { return nil }}
	result, err := engine.NewEngine(map[string]engine.Driver{"fake": host}).WithConditions(bindings, policy).Execute(t.Context(), plan, engine.ExecuteOptions{Reporting: true})
	if err != nil {
		t.Fatal(err)
	}
	progress := progressEvents(t, result)
	if len(progress) != 2 || progress[0].Scope.Instance != "one" || progress[1].Scope.Instance != "two" || progress[0].StepID == progress[1].StepID || progress[0].CheckID != progress[1].CheckID {
		t.Fatalf("local progress correlation leaked across instances: %+v", progress)
	}
}

func TestOfflineChecksCanReportWithoutReceivingInspection(t *testing.T) {
	r, target := runtimeFixture(t, func(_ context.Context, request plugin.CheckRequest, services plugin.CheckServices) (contract.Finding, error) {
		if services.Containers != nil || services.Progress == nil {
			t.Fatal("offline progress changed inspection capability")
		}
		services.Progress.Report(report.Progress{Code: "compute"})
		return contract.Finding{TargetID: request.Target.ID, CheckID: request.Check.ID, Outcome: contract.Passed, ObservedAt: time.Now(), Code: "offline"}, nil
	})
	target.Definition.Postconditions[0].Effect, target.Definition.Postconditions[0].Access = contract.Offline, nil
	bound, err := r.BindCheck(target, "ready", nil)
	if err != nil {
		t.Fatal(err)
	}
	host := &conditionHost{completed: make(map[string]bool)}
	plan := engine.Plan{APIVersion: engine.APIVersion, Kind: engine.Kind, Metadata: engine.Metadata{Name: "offline-progress"}, Steps: []engine.Step{{ID: "selected", Driver: "fake", Configuration: []byte(`"selected"`)}}}
	result, err := engine.NewEngine(map[string]engine.Driver{"fake": host}).WithConditions(map[string]engine.StepConditions{
		"selected": {Scope: engine.TargetScope{Deployment: "lab", Instance: "one", Target: "service"}, Postconditions: []engine.BoundCheck{bound}},
	}, engine.CheckPolicy{}).Execute(t.Context(), plan, engine.ExecuteOptions{Reporting: true})
	if err != nil || len(progressEvents(t, result)) != 1 {
		t.Fatalf("offline progress failed: %+v %v", result, err)
	}
}
