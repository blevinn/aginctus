package plugins_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	engine "github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/plugins"
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
