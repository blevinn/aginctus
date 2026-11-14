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
)

// This host binds only fake operations and offline checks. A logical TargetKey
// remains separate from both local finding IDs and any infrastructure authority.
type conditionHost struct{ completed map[string]bool }

func (h *conditionHost) ValidateContext(context.Context, json.RawMessage) error { return nil }
func (h *conditionHost) SupportsDryRun() bool                                   { return false }
func (h *conditionHost) Execute(_ context.Context, raw json.RawMessage, _ engine.ExecuteOptions) error {
	var key string
	if err := json.Unmarshal(raw, &key); err != nil {
		return err
	}
	h.completed[key] = true
	return nil
}

func TestScopedContributionsUseExistingConditionGatedEngine(t *testing.T) {
	testScopedConditionHost(t, false)
}

func TestScopedReadinessNeverReleasesAnotherInstancesDependent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) { testScopedConditionHost(t, true) })
}

func testScopedConditionHost(t *testing.T, polling bool) {
	t.Helper()
	ready := contract.Check{ID: "ready", Claim: "fake provider initialized", Scope: "selected fake provider", Required: true, Effect: contract.Offline, Timeout: time.Second}
	if polling {
		ready.Timeout, ready.RetryInterval = 5*time.Second, time.Second
	}
	p := contribution{discovery: newDiscovery("example"), targets: func(context.Context, json.RawMessage) ([]contract.Target, error) {
		return []contract.Target{
			{ID: "consumer", Capability: "work", DependsOn: []string{"provider"}},
			{ID: "provider", Capability: "work", Postconditions: []contract.Check{ready}},
		}, nil
	}}
	graph, err := registry(t, p).Assemble(t.Context(), "lab", []plugins.Instance{instance("b", "example"), instance("a", "example")})
	if err != nil {
		t.Fatal(err)
	}
	host := &conditionHost{completed: make(map[string]bool)}
	plan := engine.Plan{APIVersion: engine.APIVersion, Kind: engine.Kind, Metadata: engine.Metadata{Name: "fake-host"}}
	bindings := make(map[string]engine.StepConditions)
	attempts := make(map[plugins.TargetKey]int)
	for _, target := range graph.Targets {
		key := target.Key
		raw, err := json.Marshal(key.String())
		if err != nil {
			t.Fatal(err)
		}
		plan.Steps = append(plan.Steps, engine.Step{ID: key.String(), Driver: "fake-host", Configuration: raw})
		binding := engine.StepConditions{Scope: engine.TargetScope{Deployment: key.Deployment, Instance: key.Instance, Target: key.Target}}
		for _, check := range target.Definition.Postconditions {
			binding.Postconditions = append(binding.Postconditions, engine.BoundCheck{Check: check, Evaluate: func(context.Context) (contract.Finding, error) {
				attempts[key]++
				if !host.completed[key.String()] {
					t.Error("postcondition ran before its operation")
				}
				outcome := contract.Passed
				if key.Instance == "b" || (polling && attempts[key] == 1) {
					outcome = contract.Unknown
				}
				return contract.Finding{TargetID: key.Target, CheckID: check.ID, Outcome: outcome, ObservedAt: time.Now(), Code: "fake-ready"}, nil
			}})
		}
		bindings[key.String()] = binding
	}
	result, err := engine.NewEngine(map[string]engine.Driver{"fake-host": host}).WithConditions(bindings, engine.CheckPolicy{}).Execute(t.Context(), plan, engine.ExecuteOptions{})
	wantFindings := 2
	if polling {
		wantFindings = 7 // a: unknown then passed; b: five unknown attempts.
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("readiness deadline lost: %v", err)
		}
	}
	if !errors.Is(err, engine.ErrConditionNotMet) || len(result.Steps) != 2 || len(result.Operations) != 3 || len(result.Conditions) != wantFindings {
		t.Fatalf("scoped graph lost partial outcome: result=%+v error=%v", result, err)
	}
	if !host.completed["lab/a/consumer"] || host.completed["lab/b/consumer"] {
		t.Fatal("dependent ran without its own instance's required postcondition")
	}
	for i, instance := range []string{"a", "b"} {
		index := 0
		if i == 1 {
			index = len(result.Conditions) - 1
		}
		finding := result.Conditions[index]
		if finding.Scope.Instance != instance || finding.Finding.TargetID != "provider" || finding.Finding.CheckID != "ready" {
			t.Fatalf("local evidence lost host scope: %+v", finding)
		}
	}
}
