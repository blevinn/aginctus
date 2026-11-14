package orchestration

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	contract "github.com/blevinn/aginctus/pkg/orchestration"
)

var gateScope = TargetScope{Deployment: "lab", Instance: "one", Target: "target"}

func gateCheck(id string, required bool, outcome contract.Outcome) BoundCheck {
	return BoundCheck{
		Check: contract.Check{ID: id, Claim: "fixture criterion", Scope: "selected fixture", Required: required, Effect: contract.Offline, Timeout: time.Second},
		Evaluate: func(context.Context) (contract.Finding, error) {
			return contract.Finding{TargetID: gateScope.Target, CheckID: id, Outcome: outcome, ObservedAt: time.Now(), Code: "fixture"}, nil
		},
	}
}

func gateEngine(conditions map[string]StepConditions, policy CheckPolicy) (*Engine, *fakeDriver, *fakeDriver, Plan) {
	first, second := &fakeDriver{}, &fakeDriver{}
	engine := NewEngine(map[string]Driver{"first": first, "second": second}).WithConditions(conditions, policy)
	plan := testPlan(Step{ID: "first", Driver: "first", Configuration: []byte(`{}`)}, Step{ID: "second", Driver: "second", Configuration: []byte(`{}`)})
	return engine, first, second, plan
}

func TestRequiredPostconditionsBlockLaterOperations(t *testing.T) {
	for _, outcome := range []contract.Outcome{contract.Passed, contract.Failed, contract.Unknown, contract.Skipped, ""} {
		t.Run(string(outcome), func(t *testing.T) {
			engine, first, second, plan := gateEngine(map[string]StepConditions{
				"first": {Scope: gateScope, Postconditions: []BoundCheck{gateCheck("ready", true, outcome)}},
			}, CheckPolicy{})
			result, err := engine.Execute(t.Context(), plan, ExecuteOptions{})
			if outcome == contract.Passed {
				if err != nil || second.executions != 1 || len(result.Steps) != 2 {
					t.Fatalf("passed postflight: result=%+v error=%v", result, err)
				}
			} else if !errors.Is(err, ErrConditionNotMet) || second.executions != 0 || len(result.Steps) != 0 {
				t.Fatalf("unsatisfied postflight released later operation: result=%+v error=%v executions=%d", result, err, second.executions)
			}
			if first.executions != 1 || len(result.Operations) < 1 || result.Operations[0].ID != "first" || len(result.Conditions) != 1 {
				t.Fatalf("completed operation/evidence lost: %+v", result)
			}
			if outcome == "" && result.Conditions[0].Finding.Outcome != contract.Unknown {
				t.Fatal("invalid outcome was not normalized to unknown")
			}
		})
	}
}

func TestGlobalPreflightFailsBeforeAnyOperation(t *testing.T) {
	check := gateCheck("available", true, contract.Failed)
	check.Preflight = true
	engine, first, second, plan := gateEngine(map[string]StepConditions{
		"second": {Scope: gateScope, Preconditions: []BoundCheck{check}},
	}, CheckPolicy{})
	result, err := engine.Execute(t.Context(), plan, ExecuteOptions{})
	if !errors.Is(err, ErrConditionNotMet) || first.executions != 0 || second.executions != 0 || len(result.Operations) != 0 {
		t.Fatalf("mutation after failed preflight: result=%+v error=%v first=%d second=%d", result, err, first.executions, second.executions)
	}
	if result.Conditions[0].Phase != PreflightPhase || result.Conditions[0].StepID != "second" {
		t.Fatalf("incorrect evidence phase: %+v", result.Conditions)
	}
}

func TestPreflightIsRecheckedAtOperationBoundary(t *testing.T) {
	check := gateCheck("ownership", true, contract.Passed)
	check.Preflight = true
	calls := 0
	check.Evaluate = func(context.Context) (contract.Finding, error) {
		calls++
		outcome := contract.Passed
		if calls == 2 {
			outcome = contract.Failed
		}
		return contract.Finding{TargetID: gateScope.Target, CheckID: check.Check.ID, Outcome: outcome, ObservedAt: time.Now(), Code: "ownership"}, nil
	}
	engine, first, _, plan := gateEngine(map[string]StepConditions{
		"first": {Scope: gateScope, Preconditions: []BoundCheck{check}},
	}, CheckPolicy{})
	result, err := engine.Execute(t.Context(), plan, ExecuteOptions{})
	if !errors.Is(err, ErrConditionNotMet) || calls != 2 || first.executions != 0 || len(result.Conditions) != 2 || result.Conditions[1].Phase != PreconditionPhase {
		t.Fatalf("preflight bypassed boundary: result=%+v error=%v calls=%d", result, err, calls)
	}
}

func TestDeferredPreconditionsRunAfterEarlierPostconditions(t *testing.T) {
	var order []string
	check := func(id string) BoundCheck {
		c := gateCheck(id, true, contract.Passed)
		evaluate := c.Evaluate
		c.Evaluate = func(ctx context.Context) (contract.Finding, error) {
			order = append(order, id)
			return evaluate(ctx)
		}
		return c
	}
	secondScope := gateScope
	secondScope.Instance = "two"
	engine, first, second, plan := gateEngine(map[string]StepConditions{
		"first":  {Scope: gateScope, Postconditions: []BoundCheck{check("first-ready")}},
		"second": {Scope: secondScope, Preconditions: []BoundCheck{check("dependency-ready")}},
	}, CheckPolicy{})
	first.name, first.order = "first-operation", &order
	second.name, second.order = "second-operation", &order
	result, err := engine.Execute(t.Context(), plan, ExecuteOptions{})
	want := []string{"first-operation:false", "first-ready", "dependency-ready", "second-operation:false"}
	if err != nil || !reflect.DeepEqual(order, want) || len(result.Steps) != 2 || result.Conditions[1].Scope != secondScope {
		t.Fatalf("order=%v want=%v result=%+v error=%v", order, want, result, err)
	}
}

func TestOptionalFindingsRemainVisibleWithoutBlocking(t *testing.T) {
	for _, outcome := range []contract.Outcome{contract.Failed, contract.Unknown, contract.Skipped} {
		engine, _, second, plan := gateEngine(map[string]StepConditions{
			"first": {Scope: gateScope, Postconditions: []BoundCheck{gateCheck("diagnostic", false, outcome)}},
		}, CheckPolicy{})
		result, err := engine.Execute(t.Context(), plan, ExecuteOptions{})
		if err != nil || second.executions != 1 || len(result.Steps) != 2 || result.Conditions[0].Finding.Outcome != outcome {
			t.Fatalf("optional diagnostic lost or blocked: result=%+v error=%v", result, err)
		}
	}
}

func TestUnusableEvidenceCannotPassRequiredGate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		code   string
		modify func(*BoundCheck, *contract.Finding)
	}{
		{"stale", "stale-finding", func(c *BoundCheck, f *contract.Finding) {
			c.Check.MaxAge = time.Second
			f.ObservedAt = time.Now().Add(-time.Minute)
		}},
		{"cache disabled", "stale-finding", func(_ *BoundCheck, f *contract.Finding) { f.ObservedAt = time.Now().Add(-time.Second) }},
		{"future", "stale-finding", func(_ *BoundCheck, f *contract.Finding) { f.ObservedAt = time.Now().Add(time.Hour) }},
		{"target", "evidence-mismatch", func(_ *BoundCheck, f *contract.Finding) { f.TargetID = "other" }},
		{"check", "evidence-mismatch", func(_ *BoundCheck, f *contract.Finding) { f.CheckID = "other" }},
		{"incarnation", "evidence-mismatch", func(c *BoundCheck, f *contract.Finding) {
			c.ResourceIncarnation = "current"
			f.ResourceIncarnation = "previous"
		}},
		{"revision", "evidence-mismatch", func(c *BoundCheck, f *contract.Finding) {
			c.AcceptedRevision = "current"
			f.AcceptedRevision = "previous"
		}},
		{"unsafe text", "invalid-finding", func(_ *BoundCheck, f *contract.Finding) { f.Message = "raw\noutput" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check := gateCheck("ready", true, contract.Passed)
			finding, _ := check.Evaluate(t.Context())
			check.Check.MaxAge = time.Minute
			if tc.name == "cache disabled" {
				check.Check.MaxAge = 0
			}
			tc.modify(&check, &finding)
			check.Evaluate = func(context.Context) (contract.Finding, error) { return finding, nil }
			engine, _, second, plan := gateEngine(map[string]StepConditions{
				"first": {Scope: gateScope, Postconditions: []BoundCheck{check}},
			}, CheckPolicy{})
			result, err := engine.Execute(t.Context(), plan, ExecuteOptions{})
			got := result.Conditions[0].Finding
			if !errors.Is(err, ErrConditionNotMet) || second.executions != 0 || got.Outcome != contract.Unknown || got.Code != tc.code || got.Message == finding.Message {
				t.Fatalf("unusable evidence accepted/leaked: finding=%+v error=%v", got, err)
			}
		})
	}
}

func TestFreshCorrelatedCachedEvidenceCanPass(t *testing.T) {
	check := gateCheck("ready", true, contract.Passed)
	check.Check.MaxAge = time.Hour
	check.ResourceIncarnation, check.AcceptedRevision = "incarnation", "revision"
	check.Evaluate = func(context.Context) (contract.Finding, error) {
		return contract.Finding{TargetID: gateScope.Target, CheckID: "ready", Outcome: contract.Passed, ObservedAt: time.Now().Add(-time.Minute), Code: "cached", ResourceIncarnation: "incarnation", AcceptedRevision: "revision"}, nil
	}
	engine, _, _, plan := gateEngine(map[string]StepConditions{"first": {Scope: gateScope, Postconditions: []BoundCheck{check}}}, CheckPolicy{})
	if _, err := engine.Execute(t.Context(), plan, ExecuteOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestConditionDefinitionsAreValidatedBeforeDriverPreflight(t *testing.T) {
	for _, tc := range []struct {
		name   string
		modify func(map[string]StepConditions)
	}{
		{"unknown step", func(s map[string]StepConditions) { s["absent"] = s["first"] }},
		{"invalid later declaration", func(s map[string]StepConditions) {
			s["second"] = StepConditions{Scope: TargetScope{"lab", "two", "target"}, Postconditions: []BoundCheck{{}}}
		}},
		{"missing evaluator", func(s map[string]StepConditions) { s["first"].Postconditions[0].Evaluate = nil }},
		{"invalid polling interval", func(s map[string]StepConditions) {
			s["first"].Postconditions[0].Check.RetryInterval = s["first"].Postconditions[0].Check.Timeout
		}},
		{"misplaced preflight", func(s map[string]StepConditions) { s["first"].Postconditions[0].Preflight = true }},
		{"invalid evidence expectation", func(s map[string]StepConditions) { s["first"].Postconditions[0].AcceptedRevision = "raw\nrevision" }},
		{"duplicate check", func(s map[string]StepConditions) { c := s["first"]; c.Preconditions = c.Postconditions; s["first"] = c }},
		{"invalid scope", func(s map[string]StepConditions) { c := s["first"]; c.Scope.Instance = ""; s["first"] = c }},
		{"duplicate scope", func(s map[string]StepConditions) { s["second"] = s["first"] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conditions := map[string]StepConditions{"first": {Scope: gateScope, Postconditions: []BoundCheck{gateCheck("ready", true, contract.Passed)}}}
			tc.modify(conditions)
			engine, first, second, plan := gateEngine(conditions, CheckPolicy{})
			_, err := engine.Execute(t.Context(), plan, ExecuteOptions{})
			if err == nil || first.validations != 0 || second.validations != 0 || first.executions != 0 {
				t.Fatalf("incomplete plan reached driver: first=%+v second=%+v error=%v", first, second, err)
			}
		})
	}
}

func TestExternalCheckPolicyFailsClosedBeforeMutation(t *testing.T) {
	denied := errors.New("scope not authorized")
	for _, tc := range []struct {
		name   string
		policy CheckPolicy
		pass   bool
	}{
		{"offline", CheckPolicy{}, false},
		{"read-only", CheckPolicy{Mode: ReadOnlyChecks}, false},
		{"missing authorizer", CheckPolicy{Mode: ActiveChecks, OptIns: []string{"probe"}}, false},
		{"missing opt-in", CheckPolicy{Mode: ActiveChecks, Authorize: func(context.Context, TargetScope, contract.Check) error { return nil }}, false},
		{"denied", CheckPolicy{Mode: ActiveChecks, OptIns: []string{"probe"}, Authorize: func(context.Context, TargetScope, contract.Check) error { return denied }}, false},
		{"authorized", CheckPolicy{Mode: ActiveChecks, OptIns: []string{"probe"}, Authorize: func(_ context.Context, scope TargetScope, check contract.Check) error {
			if scope != gateScope || !reflect.DeepEqual(check.Access, []contract.Access{contract.GuestAccess}) {
				t.Error("authorizer did not receive scoped access requirements")
			}
			return nil
		}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check := gateCheck("ready", true, contract.Passed)
			check.Check.Effect, check.Check.Access, check.Check.OptIns = contract.ActiveProbe, []contract.Access{contract.GuestAccess}, []string{"probe"}
			calls := 0
			evaluate := check.Evaluate
			check.Evaluate = func(ctx context.Context) (contract.Finding, error) { calls++; return evaluate(ctx) }
			engine, first, _, plan := gateEngine(map[string]StepConditions{"first": {Scope: gateScope, Postconditions: []BoundCheck{check}}}, tc.policy)
			_, err := engine.Execute(t.Context(), plan, ExecuteOptions{})
			if tc.pass {
				if err != nil || calls != 1 || first.executions != 1 {
					t.Fatalf("authorized check failed: calls=%d error=%v", calls, err)
				}
			} else if err == nil || calls != 0 || first.executions != 0 {
				t.Fatalf("unauthorized probe/operation: calls=%d executions=%d error=%v", calls, first.executions, err)
			}
			if tc.name == "denied" && !errors.Is(err, denied) {
				t.Fatal("authorization cause lost")
			}
		})
	}
}

func TestConditionTimeoutCannotBecomeSuccess(t *testing.T) {
	check := gateCheck("ready", true, contract.Passed)
	check.Check.Timeout = 10 * time.Millisecond
	check.Evaluate = func(ctx context.Context) (contract.Finding, error) {
		<-ctx.Done()
		return contract.Finding{}, nil
	}
	engine, _, second, plan := gateEngine(map[string]StepConditions{"first": {Scope: gateScope, Postconditions: []BoundCheck{check}}}, CheckPolicy{})
	result, err := engine.Execute(t.Context(), plan, ExecuteOptions{})
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrConditionNotMet) || second.executions != 0 || len(result.Operations) != 1 || result.Conditions[0].Finding.Outcome != contract.Unknown {
		t.Fatalf("timed-out evaluation became success: result=%+v error=%v", result, err)
	}
}

func TestCheckAuthorizationIsBoundedBeforeAnyOperation(t *testing.T) {
	check := gateCheck("ready", true, contract.Passed)
	check.Check.Effect, check.Check.Access = contract.ReadOnly, []contract.Access{contract.IncusAccess}
	check.Check.Timeout = 10 * time.Millisecond
	evaluated := false
	check.Evaluate = func(context.Context) (contract.Finding, error) { evaluated = true; return contract.Finding{}, nil }
	policy := CheckPolicy{Mode: ReadOnlyChecks, Authorize: func(ctx context.Context, _ TargetScope, _ contract.Check) error {
		<-ctx.Done()
		return nil
	}}
	engine, first, second, plan := gateEngine(map[string]StepConditions{"second": {Scope: gateScope, Postconditions: []BoundCheck{check}}}, policy)
	_, err := engine.Execute(t.Context(), plan, ExecuteOptions{})
	if !errors.Is(err, context.DeadlineExceeded) || first.executions != 0 || second.executions != 0 || evaluated {
		t.Fatalf("authorization timeout allowed a probe/operation: error=%v", err)
	}
}

func TestHostBindingsAreDetachedAndReauthorized(t *testing.T) {
	check := gateCheck("ready", true, contract.Passed)
	check.Check.Effect, check.Check.Access, check.Check.OptIns = contract.ReadOnly, []contract.Access{contract.GuestAccess}, []string{"inspect"}
	conditions := map[string]StepConditions{"first": {Scope: gateScope, Postconditions: []BoundCheck{check}}}
	denied := errors.New("authorization revoked")
	calls := 0
	policy := CheckPolicy{Mode: ReadOnlyChecks, OptIns: []string{"inspect"}, Authorize: func(_ context.Context, scope TargetScope, definition contract.Check) error {
		calls++
		if scope != gateScope || definition.ID != "ready" || definition.Access[0] != contract.GuestAccess || definition.OptIns[0] != "inspect" {
			t.Error("caller/authorizer mutated stored binding metadata")
		}
		definition.Access[0], definition.OptIns[0] = contract.HostAccess, "changed"
		if calls == 2 {
			return denied
		}
		return nil
	}}
	engine, first, second, plan := gateEngine(conditions, policy)
	conditions["first"].Postconditions[0].Check.ID = "changed"
	conditions["first"].Postconditions[0].Check.Access[0] = contract.NetworkAccess
	conditions["first"].Postconditions[0].Check.OptIns[0] = "changed"
	conditions["absent"] = conditions["first"]
	policy.OptIns[0] = "changed"
	result, err := engine.Execute(t.Context(), plan, ExecuteOptions{})
	if !errors.Is(err, ErrConditionNotMet) || !errors.Is(err, denied) || calls != 2 || first.executions != 1 || second.executions != 0 || result.Conditions[0].Finding.Code != "authorization-denied" {
		t.Fatalf("authorization was cached or metadata aliased: result=%+v calls=%d error=%v", result, calls, err)
	}
}

func TestEvaluationErrorsAreUnknownAndPreserveRequiredCause(t *testing.T) {
	cause := errors.New("fixture evaluation failed")
	for _, required := range []bool{false, true} {
		check := gateCheck("ready", required, contract.Passed)
		check.Evaluate = func(context.Context) (contract.Finding, error) { return contract.Finding{}, cause }
		engine, _, second, plan := gateEngine(map[string]StepConditions{"first": {Scope: gateScope, Postconditions: []BoundCheck{check}}}, CheckPolicy{})
		result, err := engine.Execute(t.Context(), plan, ExecuteOptions{})
		if required {
			if !errors.Is(err, cause) || !errors.Is(err, ErrConditionNotMet) || second.executions != 0 {
				t.Fatalf("required cause lost: %v", err)
			}
		} else if err != nil || second.executions != 1 {
			t.Fatalf("optional error blocked: %v", err)
		}
		if got := result.Conditions[0].Finding; got.Outcome != contract.Unknown || strings.Contains(got.Message, cause.Error()) {
			t.Fatalf("unsafe or positive error finding: %+v", got)
		}
	}
}

func TestCancellationDuringConditionsPreservesPartialOutcome(t *testing.T) {
	for _, postflight := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		check := gateCheck("blocked", false, contract.Passed)
		started := make(chan struct{})
		check.Evaluate = func(ctx context.Context) (contract.Finding, error) {
			close(started)
			<-ctx.Done()
			return contract.Finding{}, nil
		}
		conditions := StepConditions{Scope: gateScope, Preconditions: []BoundCheck{check}}
		if postflight {
			conditions.Preconditions, conditions.Postconditions = nil, []BoundCheck{check}
		}
		engine, first, second, plan := gateEngine(map[string]StepConditions{"first": conditions}, CheckPolicy{})
		type response struct {
			result Result
			err    error
		}
		finished := make(chan response, 1)
		go func() { result, err := engine.Execute(ctx, plan, ExecuteOptions{}); finished <- response{result, err} }()
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			cancel()
			t.Fatal("condition did not start")
		}
		cancel()
		select {
		case got := <-finished:
			want := 0
			if postflight {
				want = 1
			}
			if !errors.Is(got.err, context.Canceled) || first.executions != want || second.executions != 0 || len(got.result.Steps) != 0 || len(got.result.Operations) != want {
				t.Fatalf("cancellation lost partial outcome: result=%+v error=%v", got.result, got.err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("condition ignored cancellation")
		}
	}
}

func TestConditionDryRunDoesNotProbeOrExecute(t *testing.T) {
	engine, first, _, plan := gateEngine(map[string]StepConditions{"first": {Scope: gateScope, Postconditions: []BoundCheck{gateCheck("ready", true, contract.Passed)}}}, CheckPolicy{})
	_, err := engine.Execute(t.Context(), plan, ExecuteOptions{DryRun: true})
	if err == nil || !strings.Contains(err.Error(), "dry-run") || first.validations != 0 || first.executions != 0 {
		t.Fatalf("condition dry-run reached driver: error=%v driver=%+v", err, first)
	}
}
