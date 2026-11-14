package orchestration

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	collection "github.com/blevinn/aginctus/internal/observability"
	report "github.com/blevinn/aginctus/pkg/observability"
	contract "github.com/blevinn/aginctus/pkg/orchestration"
)

// TargetScope correlates evidence with one logical deployment/instance/target.
// It is not an Incus scope or a grant of infrastructure authority.
type TargetScope struct{ Deployment, Instance, Target string }

type CheckMode string

const (
	OfflineChecks  CheckMode = ""
	ReadOnlyChecks CheckMode = "read-only"
	ActiveChecks   CheckMode = "active"
)

// CheckPolicy limits check effects, independently of operation authorization.
// Authorize must approve every declared access requirement in the supplied scope.
// It is a policy check, not a dependency-produced resource precondition. The host
// binds appropriately scoped services in Evaluate; no ambient client is supplied.
type CheckPolicy struct {
	Mode      CheckMode
	OptIns    []string
	Authorize func(context.Context, TargetScope, contract.Check) error
}

// BoundCheck binds a public declaration to a trusted host evaluator. Preflight
// additionally runs a precondition before all operations; it is still rechecked
// at its operation boundary. Leave dependency-produced preconditions deferred.
// Timeout bounds the entire readiness window, including authorization and waits.
// Evaluators/authorizers must emit nonsecret findings and safe errors: text
// validation is not secret redaction, and wrapped error causes remain observable.
type BoundCheck struct {
	Check               contract.Check
	Evaluate            func(context.Context) (contract.Finding, error)
	Preflight           bool
	ResourceIncarnation string
	AcceptedRevision    string
}

type StepConditions struct {
	Scope          TargetScope
	Preconditions  []BoundCheck
	Postconditions []BoundCheck
}

type ConditionPhase string

const (
	PreflightPhase     ConditionPhase = "preflight"
	PreconditionPhase  ConditionPhase = "precondition"
	PostconditionPhase ConditionPhase = "postcondition"
)

type ConditionResult struct {
	StepID string
	Scope  TargetScope
	Phase  ConditionPhase
	// Attempt is one-based within a step/phase/check evaluation window. Every
	// actual attempt is retained; a deadline during a wait adds no observation.
	Attempt int
	Finding contract.Finding
}

var ErrConditionNotMet = errors.New("required orchestration condition not met")
var scopeIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// WithConditions returns a detached engine with explicit host-only bindings.
// Bindings are not accepted through serialized plans. The existing ordered
// execution loop remains the only executor; it stops before any later step when
// a required condition fails. Callers must not mutate callback closure state
// concurrently or treat a passed check as enduring write authorization.
func (e *Engine) WithConditions(steps map[string]StepConditions, policy CheckPolicy) *Engine {
	bound := NewEngine(e.drivers)
	bound.conditions = make(map[string]StepConditions, len(steps))
	for id, step := range steps {
		step.Preconditions = cloneBoundChecks(step.Preconditions)
		step.Postconditions = cloneBoundChecks(step.Postconditions)
		bound.conditions[id] = step
	}
	policy.OptIns = slices.Clone(policy.OptIns)
	bound.checkPolicy = policy
	return bound
}

func cloneBoundChecks(checks []BoundCheck) []BoundCheck {
	checks = slices.Clone(checks)
	for i := range checks {
		checks[i].Check.Access = slices.Clone(checks[i].Check.Access)
		checks[i].Check.OptIns = slices.Clone(checks[i].Check.OptIns)
	}
	return checks
}

func (e *Engine) validateConditions(plan Plan, options ExecuteOptions) error {
	if e.conditions == nil {
		return nil
	}
	if options.DryRun {
		return fmt.Errorf("condition-gated execution does not support dry-run")
	}
	if e.checkPolicy.Mode != OfflineChecks && e.checkPolicy.Mode != ReadOnlyChecks && e.checkPolicy.Mode != ActiveChecks {
		return fmt.Errorf("unknown check effect policy")
	}
	seen := make(map[string]bool, len(plan.Steps))
	scopes := make(map[TargetScope]bool)
	for _, step := range plan.Steps {
		seen[step.ID] = true
		conditions, ok := e.conditions[step.ID]
		if !ok {
			continue
		}
		scope := conditions.Scope
		if !scopeIdentifier.MatchString(scope.Deployment) || !scopeIdentifier.MatchString(scope.Instance) || !scopeIdentifier.MatchString(scope.Target) || scopes[scope] {
			return fmt.Errorf("step %q requires a unique valid condition target scope", step.ID)
		}
		scopes[scope] = true
		ids := make(map[string]bool)
		for phase, checks := range [][]BoundCheck{conditions.Preconditions, conditions.Postconditions} {
			for _, check := range checks {
				if err := check.Check.Validate(); err != nil {
					return fmt.Errorf("step %q condition: %w", step.ID, err)
				}
				// Expectations must themselves be representable by a valid
				// finding; reject impossible identity bindings before mutation.
				expected := contract.Finding{TargetID: scope.Target, CheckID: check.Check.ID,
					Outcome: contract.Unknown, ObservedAt: time.Unix(1, 0), Code: "expected-evidence",
					ResourceIncarnation: check.ResourceIncarnation, AcceptedRevision: check.AcceptedRevision}
				if err := expected.Validate(); err != nil {
					return fmt.Errorf("step %q condition evidence expectation: %w", step.ID, err)
				}
				if ids[check.Check.ID] || check.Evaluate == nil || (phase == 1 && check.Preflight) {
					return fmt.Errorf("step %q has duplicate, unbound or misplaced conditions", step.ID)
				}
				ids[check.Check.ID] = true
				if err := e.checkAllowed(check.Check); err != nil {
					return fmt.Errorf("step %q condition %q: %w", step.ID, check.Check.ID, err)
				}
			}
		}
	}
	for id := range e.conditions {
		if !seen[id] {
			return fmt.Errorf("condition binding refers to unknown step %q", id)
		}
	}
	return nil
}

func (e *Engine) checkAllowed(check contract.Check) error {
	if check.Effect == contract.Offline {
		return nil
	}
	if e.checkPolicy.Mode == OfflineChecks || (check.Effect == contract.ActiveProbe && e.checkPolicy.Mode != ActiveChecks) {
		return fmt.Errorf("check effect is prohibited by host policy")
	}
	if e.checkPolicy.Authorize == nil {
		return fmt.Errorf("external check requires scoped host authorization")
	}
	for _, required := range check.OptIns {
		if !slices.Contains(e.checkPolicy.OptIns, required) {
			return fmt.Errorf("check requires host opt-in %q", required)
		}
	}
	return nil
}

// authorizeConditions checks the complete selection before any evaluator or
// operation, then runCheck repeats authorization at each actual probe boundary.
func (e *Engine) authorizeConditions(ctx context.Context, plan Plan) error {
	for _, step := range plan.Steps {
		conditions := e.conditions[step.ID]
		for _, checks := range [][]BoundCheck{conditions.Preconditions, conditions.Postconditions} {
			for _, check := range checks {
				authorizeCtx, cancel := context.WithTimeout(ctx, check.Check.Timeout)
				err := e.authorizeCheck(authorizeCtx, conditions.Scope, check.Check)
				cancel()
				if err != nil {
					return fmt.Errorf("authorize step %q condition %q: %w", step.ID, check.Check.ID, err)
				}
			}
		}
	}
	return ctx.Err()
}

func (e *Engine) authorizeCheck(ctx context.Context, scope TargetScope, check contract.Check) error {
	if err := conditionContextErr(ctx); err != nil {
		return err
	}
	if check.Effect == contract.Offline {
		return nil
	}
	// A trusted authorizer may inspect the declaration, but cannot mutate the
	// stored metadata used by subsequent validation, authorization and findings.
	check.Access = slices.Clone(check.Access)
	check.OptIns = slices.Clone(check.OptIns)
	return errors.Join(e.checkPolicy.Authorize(ctx, scope, check), conditionContextErr(ctx))
}

// A deadline timer may be due before its cancellation goroutine has run. Do not
// start another probe or accept late evidence in that timer-delivery gap.
func conditionContextErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

func (e *Engine) runConditions(ctx context.Context, step Step, phase ConditionPhase, result *Result) error {
	conditions := e.conditions[step.ID]
	checks := conditions.Preconditions
	if phase == PostconditionPhase {
		checks = conditions.Postconditions
	}
	for _, check := range checks {
		if phase == PreflightPhase && !check.Preflight {
			continue
		}
		if err := conditionContextErr(ctx); err != nil {
			return err
		}
		finding, cause := e.runCondition(ctx, step.ID, conditions.Scope, phase, check, result)
		if err := conditionContextErr(ctx); err != nil {
			return fmt.Errorf("step %q condition %q canceled: %w", step.ID, check.Check.ID, errors.Join(err, cause))
		}
		if check.Check.Required && finding.Outcome != contract.Passed {
			return fmt.Errorf("step %q %s condition %q (%s): %w", step.ID, phase, check.Check.ID, finding.Outcome, errors.Join(ErrConditionNotMet, cause))
		}
	}
	return nil
}

// runCondition retries evidence, never the operation. Only usable failed/unknown
// findings are retryable: denial, evaluator errors, unusable evidence and skipped
// checks are terminal. Optional checks use the same bounded window, but only
// parent cancellation prevents later operations after an optional non-pass.
func (e *Engine) runCondition(ctx context.Context, stepID string, scope TargetScope, phase ConditionPhase, check BoundCheck, result *Result) (finding contract.Finding, cause error) {
	window, cancel := context.WithTimeout(ctx, check.Check.Timeout)
	defer cancel()
	result.checkEvent(stepID, scope, phase, check, report.Condition, report.Running, 0, contract.Finding{}, nil)
	defer func() {
		state := findingState(finding)
		if conditionContextErr(ctx) != nil {
			state = report.Canceled
		}
		result.checkEvent(stepID, scope, phase, check, report.Condition, state, 0, finding, cause)
	}()
	for attempt := 1; ; attempt++ {
		if err := conditionContextErr(window); err != nil && attempt > 1 {
			return finding, err
		}
		result.checkEvent(stepID, scope, phase, check, report.CheckAttempt, report.Running, attempt, contract.Finding{}, nil)
		attemptCtx, done := context.WithCancel(window)
		var collector *collection.Collector
		var progressEvent report.Event
		if result.reporting != nil {
			collector = result.reporting.collector
			id := result.reporting.steps[stepID]
			progressEvent = report.Event{StepID: id, OperationID: id,
				Scope: report.Scope{Deployment: scope.Deployment, Instance: scope.Instance, Target: scope.Target},
				Phase: string(phase), CheckID: check.Check.ID, Attempt: attempt,
				Required: check.Check.Required, AcceptedRevision: check.AcceptedRevision}
		}
		attemptCtx = collection.WithProgress(attemptCtx, collector, progressEvent)
		observed, cause, retry := e.evaluateCheck(attemptCtx, scope, check)
		done()
		finding = observed
		result.checkEvent(stepID, scope, phase, check, report.CheckAttempt, findingState(finding), attempt, finding, cause)
		result.Conditions = append(result.Conditions, ConditionResult{StepID: stepID, Scope: scope, Phase: phase, Attempt: attempt, Finding: finding})
		if !retry || check.Check.RetryInterval == 0 {
			return finding, cause
		}
		result.checkEvent(stepID, scope, phase, check, report.Condition, report.Waiting, attempt, finding, nil)
		timer := time.NewTimer(check.Check.RetryInterval)
		select {
		case <-window.Done():
			timer.Stop()
			return finding, window.Err()
		case <-timer.C:
		}
	}
}

// evaluateCheck performs one authorized evaluation within the shared deadline.
// The retry flag is based on validated evidence, not caller-controlled codes.
func (e *Engine) evaluateCheck(ctx context.Context, scope TargetScope, check BoundCheck) (contract.Finding, error, bool) {
	unknown := func(code string) contract.Finding {
		return contract.Finding{TargetID: scope.Target, CheckID: check.Check.ID, Outcome: contract.Unknown,
			ObservedAt: time.Now(), Code: code, Message: "Condition did not produce usable evidence."}
	}
	if err := e.authorizeCheck(ctx, scope, check.Check); err != nil {
		return unknown("authorization-denied"), err, false
	}
	started := time.Now()
	finding, evaluateErr := check.Evaluate(ctx)
	if err := errors.Join(evaluateErr, conditionContextErr(ctx)); err != nil {
		return unknown("evaluation-error"), err, false
	}
	now := time.Now()
	if err := finding.Validate(); err != nil {
		return unknown("invalid-finding"), err, false
	}
	if finding.TargetID != scope.Target || finding.CheckID != check.Check.ID ||
		(check.ResourceIncarnation != "" && finding.ResourceIncarnation != check.ResourceIncarnation) ||
		(check.AcceptedRevision != "" && finding.AcceptedRevision != check.AcceptedRevision) {
		return unknown("evidence-mismatch"), nil, false
	}
	if finding.ObservedAt.After(now) || (check.Check.MaxAge == 0 && finding.ObservedAt.Before(started)) ||
		(check.Check.MaxAge > 0 && now.Sub(finding.ObservedAt) > check.Check.MaxAge) {
		return unknown("stale-finding"), nil, false
	}
	if err := conditionContextErr(ctx); err != nil {
		return unknown("evaluation-error"), err, false
	}
	return finding, nil, finding.Outcome == contract.Failed || finding.Outcome == contract.Unknown
}
