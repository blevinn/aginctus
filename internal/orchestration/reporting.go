package orchestration

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	collection "github.com/blevinn/aginctus/internal/observability"
	report "github.com/blevinn/aginctus/pkg/observability"
	contract "github.com/blevinn/aginctus/pkg/orchestration"
)

type executionReport struct {
	collector *collection.Collector
	steps     map[string]string
	phase     report.Kind
	active    report.Event
	operation bool
	dryRun    bool
}

func newExecutionReport(plan Plan, dryRun bool, monitor *collection.Monitor) *executionReport {
	collector := monitor.Claim()
	if collector == nil {
		collector = collection.New(rand.Text())
	}
	r := &executionReport{collector: collector, steps: make(map[string]string), dryRun: dryRun}
	for i, step := range plan.Steps {
		r.steps[step.ID] = fmt.Sprintf("step-%d", i+1)
	}
	r.collector.Record(report.Event{Kind: report.Run, State: report.Running})
	return r
}

func (r *Result) phaseStarted(kind report.Kind) {
	if r.reporting != nil {
		r.reporting.phase = kind
		r.reporting.collector.Record(report.Event{Kind: kind, State: report.Running})
	}
}

func (r *Result) phaseFinished() {
	if r.reporting != nil {
		r.reporting.collector.Record(report.Event{Kind: r.reporting.phase, State: report.Completed})
		r.reporting.phase = ""
	}
}

func (r *Result) stepStarted(step Step, scope TargetScope) {
	if r.reporting != nil {
		id := r.reporting.steps[step.ID]
		r.reporting.active = report.Event{Kind: report.Step, State: report.Running, StepID: id, OperationID: id,
			Scope: report.Scope{Deployment: scope.Deployment, Instance: scope.Instance, Target: scope.Target}}
		r.reporting.collector.Record(r.reporting.active)
	}
}

func (r *Result) operationStarted() {
	if r.reporting != nil {
		r.reporting.operation = true
		event := r.reporting.active
		event.Kind = report.Operation
		r.reporting.collector.Record(event)
	}
}

func (r *Result) operationFinished() {
	if r.reporting != nil {
		event := r.reporting.active
		event.Kind, event.State = report.Operation, r.reporting.success()
		r.reporting.collector.Record(event)
		r.reporting.operation = false
	}
}

// Native effect evidence distinguishes a denial or failed observation from an
// uncertain submission; legacy drivers do not expose this boundary.
func (r *Result) nativeOperationFailed(ctx context.Context, firstEffect int, err error) {
	if r.reporting == nil || r.Native == nil {
		return
	}
	for _, effect := range r.Native.Effects[firstEffect:] {
		if effect.Outcome == NativeEffectUncertain {
			return // finish records the uncertain operation and run.
		}
	}
	event := r.reporting.active
	event.Kind, event.State, event.Reason = report.Operation, report.Failed, errorReason(err)
	if conditionContextErr(ctx) != nil {
		event.State = report.Canceled
	}
	r.reporting.collector.Record(event)
	r.reporting.operation = false
}

func (r *executionReport) verification(state report.State, err error) {
	event := r.active
	event.Kind, event.State, event.Reason = report.PostconditionVerification, state, errorReason(err)
	r.collector.Record(event)
}

func (r *Result) stepFinished() {
	if r.reporting != nil {
		event := r.reporting.active
		event.State = r.reporting.success()
		r.reporting.collector.Record(event)
		r.reporting.active = report.Event{}
	}
}

func (r *executionReport) success() report.State {
	if r.dryRun {
		return report.Simulated
	}
	return report.Completed
}

func errorReason(err error) report.Reason {
	switch {
	case errors.Is(err, context.Canceled):
		return report.Cancellation
	case errors.Is(err, context.DeadlineExceeded):
		return report.Deadline
	case errors.Is(err, ErrConditionNotMet):
		return report.ConditionGate
	case err != nil:
		return report.Error
	default:
		return report.NoReason
	}
}

func (r *executionReport) finish(ctx context.Context, err error) report.Report {
	state := r.success()
	if err != nil {
		state = report.Failed
		if conditionContextErr(ctx) != nil {
			state = report.Canceled
		} else if errors.Is(err, ErrConditionNotMet) {
			state = report.Blocked
		}
	}
	if r.operation {
		// Once a real driver was entered, an error/cancellation is not proof
		// that no write occurred. Dry-run never claims uncertain mutation.
		if !r.dryRun {
			state = report.Uncertain
		}
		event := r.active
		event.Kind, event.State, event.Reason = report.Operation, state, errorReason(err)
		r.collector.Record(event)
	}
	if r.active.Kind != "" {
		event := r.active
		event.State, event.Reason = state, errorReason(err)
		r.collector.Record(event)
	}
	if r.phase != "" {
		r.collector.Record(report.Event{Kind: r.phase, State: state, Reason: errorReason(err)})
	}
	r.collector.Record(report.Event{Kind: report.Run, State: state, Reason: errorReason(err)})
	return r.collector.Report()
}

func (r *Result) checkEvent(stepID string, scope TargetScope, phase ConditionPhase, check BoundCheck, kind report.Kind, state report.State, attempt int, finding contract.Finding, cause error) {
	if r.reporting == nil {
		return
	}
	id := r.reporting.steps[stepID]
	r.reporting.collector.Record(report.Event{Kind: kind, State: state, StepID: id, OperationID: id,
		Scope: report.Scope{Deployment: scope.Deployment, Instance: scope.Instance, Target: scope.Target},
		Phase: string(phase), CheckID: check.Check.ID, Attempt: attempt, Required: check.Check.Required,
		AcceptedRevision: check.AcceptedRevision, ObservedAt: finding.ObservedAt, Reason: errorReason(cause)})
}

func findingState(finding contract.Finding) report.State {
	switch finding.Outcome {
	case contract.Passed:
		return report.Passed
	case contract.Failed:
		return report.Failed
	case contract.Skipped:
		return report.Skipped
	default:
		return report.Unknown
	}
}
