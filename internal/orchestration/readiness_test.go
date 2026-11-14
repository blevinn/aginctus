package orchestration

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	contract "github.com/blevinn/aginctus/pkg/orchestration"
)

func readinessCheck(required bool) BoundCheck {
	check := gateCheck("ready", required, contract.Passed)
	check.Check.Timeout, check.Check.RetryInterval = 5*time.Second, time.Second
	return check
}

func TestReadinessReleasesDependentOnlyAfterPassedEvidence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		check := readinessCheck(true)
		calls := 0
		var times []time.Time
		check.Evaluate = func(context.Context) (contract.Finding, error) {
			times = append(times, time.Now())
			outcomes := []contract.Outcome{contract.Failed, contract.Unknown, contract.Passed}
			finding := contract.Finding{TargetID: gateScope.Target, CheckID: "ready", Outcome: outcomes[calls], ObservedAt: time.Now(), Code: "fixture"}
			calls++
			return finding, nil
		}
		engine, first, second, plan := gateEngine(map[string]StepConditions{"first": {Scope: gateScope, Postconditions: []BoundCheck{check}}}, CheckPolicy{})
		result, err := engine.Execute(t.Context(), plan, ExecuteOptions{})
		if err != nil || calls != 3 || first.executions != 1 || second.executions != 1 || len(result.Steps) != 2 || len(result.Conditions) != 3 {
			t.Fatalf("readiness did not gate dependent: result=%+v calls=%d error=%v", result, calls, err)
		}
		for i, observed := range result.Conditions {
			if observed.Attempt != i+1 || observed.Scope != gateScope || observed.Phase != PostconditionPhase || observed.StepID != "first" {
				t.Fatalf("attempt identity lost: %+v", observed)
			}
			if i > 0 && times[i].Sub(times[i-1]) != time.Second {
				t.Fatalf("retry interval not respected: %v", times)
			}
		}
	})
}

func TestReadinessDeadlineDuringWaitPreservesLastObservation(t *testing.T) {
	for _, required := range []bool{true, false} {
		synctest.Test(t, func(t *testing.T) {
			check := readinessCheck(required)
			evaluate := gateCheck("ready", required, contract.Unknown).Evaluate
			calls := 0
			check.Evaluate = func(ctx context.Context) (contract.Finding, error) { calls++; return evaluate(ctx) }
			engine, first, second, plan := gateEngine(map[string]StepConditions{"first": {Scope: gateScope, Postconditions: []BoundCheck{check}}}, CheckPolicy{})
			started := time.Now()
			result, err := engine.Execute(t.Context(), plan, ExecuteOptions{})
			if time.Since(started) != 5*time.Second || calls != 5 || len(result.Conditions) != 5 || first.executions != 1 {
				t.Fatalf("deadline reset or invented evidence: elapsed=%v calls=%d result=%+v", time.Since(started), calls, result)
			}
			if required {
				if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrConditionNotMet) || second.executions != 0 || len(result.Operations) != 1 || len(result.Steps) != 0 {
					t.Fatalf("deadline released dependent or lost operation: result=%+v error=%v", result, err)
				}
			} else if err != nil || second.executions != 1 || len(result.Steps) != 2 {
				t.Fatalf("optional readiness blocked: result=%+v error=%v", result, err)
			}
			last := result.Conditions[4]
			if last.Attempt != 5 || last.Finding.Outcome != contract.Unknown || last.Finding.Code != "fixture" || last.Finding.ObservedAt.Sub(started) != 4*time.Second {
				t.Fatalf("last real observation lost: %+v", last)
			}
		})
	}
}

func TestReadinessAuthorizationAndEvaluationShareOneDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		check := readinessCheck(true)
		check.Check.Effect, check.Check.Access = contract.ReadOnly, []contract.Access{contract.GuestAccess}
		var deadlines []time.Time
		policy := CheckPolicy{Mode: ReadOnlyChecks, Authorize: func(ctx context.Context, _ TargetScope, _ contract.Check) error {
			deadline, _ := ctx.Deadline()
			deadlines = append(deadlines, deadline)
			time.Sleep(time.Second)
			return nil
		}}
		calls := 0
		check.Evaluate = func(ctx context.Context) (contract.Finding, error) {
			calls++
			deadline, _ := ctx.Deadline()
			deadlines = append(deadlines, deadline)
			if calls == 1 {
				time.Sleep(time.Second)
				return contract.Finding{TargetID: gateScope.Target, CheckID: "ready", Outcome: contract.Failed, ObservedAt: time.Now(), Code: "starting"}, nil
			}
			<-ctx.Done()
			// Even a positive response after timeout cannot establish readiness.
			return contract.Finding{TargetID: gateScope.Target, CheckID: "ready", Outcome: contract.Passed, ObservedAt: time.Now(), Code: "late"}, nil
		}
		engine, first, second, plan := gateEngine(map[string]StepConditions{"first": {Scope: gateScope, Postconditions: []BoundCheck{check}}}, policy)
		started := time.Now()
		result, err := engine.Execute(t.Context(), plan, ExecuteOptions{})
		// Selection authorization has its own budget; all per-attempt work
		// shares the five-second window beginning after the operation.
		if time.Since(started) != 6*time.Second || calls != 2 || first.executions != 1 || second.executions != 0 || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("unbounded readiness: elapsed=%v calls=%d error=%v", time.Since(started), calls, err)
		}
		if len(deadlines) != 5 || !reflect.DeepEqual(deadlines[1:], []time.Time{deadlines[1], deadlines[1], deadlines[1], deadlines[1]}) {
			t.Fatalf("attempts got fresh budgets: %v", deadlines)
		}
		if len(result.Conditions) != 2 || result.Conditions[1].Finding.Outcome != contract.Unknown || result.Conditions[1].Finding.Code != "evaluation-error" {
			t.Fatalf("late success accepted: %+v", result)
		}
	})
}

func TestReadinessRevokedAuthorizationIsTerminal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		check := readinessCheck(true)
		check.Check.Effect, check.Check.Access = contract.ReadOnly, []contract.Access{contract.GuestAccess}
		denied := errors.New("revoked")
		authorizations, evaluations := 0, 0
		policy := CheckPolicy{Mode: ReadOnlyChecks, Authorize: func(context.Context, TargetScope, contract.Check) error {
			authorizations++
			if authorizations == 3 {
				return denied
			}
			return nil
		}}
		evaluate := gateCheck("ready", true, contract.Unknown).Evaluate
		check.Evaluate = func(ctx context.Context) (contract.Finding, error) { evaluations++; return evaluate(ctx) }
		engine, first, second, plan := gateEngine(map[string]StepConditions{"first": {Scope: gateScope, Postconditions: []BoundCheck{check}}}, policy)
		result, err := engine.Execute(t.Context(), plan, ExecuteOptions{})
		if !errors.Is(err, denied) || !errors.Is(err, ErrConditionNotMet) || authorizations != 3 || evaluations != 1 || first.executions != 1 || second.executions != 0 || len(result.Conditions) != 2 || result.Conditions[1].Finding.Code != "authorization-denied" {
			t.Fatalf("revocation retried/bypassed: result=%+v error=%v", result, err)
		}
	})
}

func TestReadinessDoesNotRetryUnusableEvidenceOrErrors(t *testing.T) {
	cause := errors.New("evaluation failed")
	for _, name := range []string{"skipped", "invalid", "mismatch", "stale", "error"} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				check := readinessCheck(true)
				calls := 0
				check.Evaluate = func(context.Context) (contract.Finding, error) {
					calls++
					finding := contract.Finding{TargetID: gateScope.Target, CheckID: "ready", Outcome: contract.Passed, ObservedAt: time.Now(), Code: "fixture"}
					if calls == 1 {
						finding.Outcome = contract.Unknown
						finding.ResourceIncarnation = check.ResourceIncarnation
						return finding, nil
					}
					switch name {
					case "skipped":
						finding.Outcome = contract.Skipped
					case "invalid":
						finding.Outcome = ""
					case "mismatch":
						finding.ResourceIncarnation = "previous"
					case "stale":
						finding.ObservedAt = finding.ObservedAt.Add(-time.Second)
					case "error":
						return finding, cause
					}
					return finding, nil
				}
				if name == "mismatch" {
					check.ResourceIncarnation = "current"
				}
				engine, _, second, plan := gateEngine(map[string]StepConditions{"first": {Scope: gateScope, Postconditions: []BoundCheck{check}}}, CheckPolicy{})
				started := time.Now()
				result, err := engine.Execute(t.Context(), plan, ExecuteOptions{})
				if !errors.Is(err, ErrConditionNotMet) || calls != 2 || second.executions != 0 || len(result.Conditions) != 2 || time.Since(started) != time.Second {
					t.Fatalf("terminal result retried: result=%+v calls=%d error=%v", result, calls, err)
				}
				if name == "error" && !errors.Is(err, cause) {
					t.Fatal("evaluation cause lost")
				}
			})
		})
	}
}

func TestPollingGlobalPreflightTimesOutBeforeAnyOperation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		check := readinessCheck(true)
		check.Preflight = true
		check.Evaluate = gateCheck("ready", true, contract.Unknown).Evaluate
		engine, first, second, plan := gateEngine(map[string]StepConditions{"second": {Scope: gateScope, Preconditions: []BoundCheck{check}}}, CheckPolicy{})
		result, err := engine.Execute(t.Context(), plan, ExecuteOptions{})
		if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrConditionNotMet) || first.executions != 0 || second.executions != 0 || len(result.Operations) != 0 || len(result.Conditions) != 5 {
			t.Fatalf("polling preflight allowed mutation: result=%+v error=%v", result, err)
		}
	})
}

func TestParentDeadlineStopsOptionalReadiness(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		defer cancel()
		check := readinessCheck(false)
		check.Evaluate = gateCheck("ready", false, contract.Unknown).Evaluate
		engine, first, second, plan := gateEngine(map[string]StepConditions{"first": {Scope: gateScope, Postconditions: []BoundCheck{check}}}, CheckPolicy{})
		started := time.Now()
		result, err := engine.Execute(ctx, plan, ExecuteOptions{})
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) != 500*time.Millisecond || first.executions != 1 || second.executions != 0 || len(result.Operations) != 1 || len(result.Steps) != 0 || len(result.Conditions) != 1 {
			t.Fatalf("optional polling outlived caller deadline: result=%+v error=%v", result, err)
		}
	})
}

func TestCancellationDuringReadinessWaitStopsOptionalChecks(t *testing.T) {
	for _, postflight := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			time.AfterFunc(500*time.Millisecond, cancel)
			check := readinessCheck(false)
			check.Evaluate = gateCheck("ready", false, contract.Unknown).Evaluate
			conditions := StepConditions{Scope: gateScope, Preconditions: []BoundCheck{check}}
			if postflight {
				conditions.Preconditions, conditions.Postconditions = nil, []BoundCheck{check}
			}
			engine, first, second, plan := gateEngine(map[string]StepConditions{"first": conditions}, CheckPolicy{})
			started := time.Now()
			result, err := engine.Execute(ctx, plan, ExecuteOptions{})
			want := 0
			if postflight {
				want = 1
			}
			if !errors.Is(err, context.Canceled) || time.Since(started) != 500*time.Millisecond || first.executions != want || second.executions != 0 || len(result.Operations) != want || len(result.Steps) != 0 || len(result.Conditions) != 1 {
				t.Fatalf("wait ignored cancellation/lost partial outcome: result=%+v error=%v", result, err)
			}
		})
	}
}

func TestPollingPreflightIsReevaluatedAtMutationBoundary(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		check := readinessCheck(true)
		check.Preflight = true
		calls := 0
		check.Evaluate = func(context.Context) (contract.Finding, error) {
			calls++
			outcome := contract.Passed
			if calls > 1 {
				outcome = contract.Failed
			}
			return contract.Finding{TargetID: gateScope.Target, CheckID: "ready", Outcome: outcome, ObservedAt: time.Now(), Code: "fixture"}, nil
		}
		engine, first, second, plan := gateEngine(map[string]StepConditions{"first": {Scope: gateScope, Preconditions: []BoundCheck{check}}}, CheckPolicy{})
		result, err := engine.Execute(t.Context(), plan, ExecuteOptions{})
		if !errors.Is(err, context.DeadlineExceeded) || first.executions != 0 || second.executions != 0 || len(result.Conditions) != 6 || result.Conditions[0].Phase != PreflightPhase || result.Conditions[1].Phase != PreconditionPhase || result.Conditions[1].Attempt != 1 {
			t.Fatalf("preflight bypassed fresh boundary window: result=%+v error=%v", result, err)
		}
	})
}
