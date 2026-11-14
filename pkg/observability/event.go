// Package observability defines experimental, nonsecret run-reporting contracts.
// Events describe execution, not current health, authorization or durable state.
package observability

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const SchemaVersion = "aginctus.dev/events/v1alpha1"

type Kind string

const (
	Run           Kind = "run"
	Validation    Kind = "validation"
	Authorization Kind = "authorization"
	Preflight     Kind = "preflight"
	Step          Kind = "step"
	Operation     Kind = "operation"
	Condition     Kind = "condition"
	CheckAttempt  Kind = "check-attempt"
)

type State string

const (
	Running   State = "running"
	Completed State = "completed"
	Simulated State = "simulated"
	Passed    State = "passed"
	Failed    State = "failed"
	Unknown   State = "unknown"
	Skipped   State = "skipped"
	Waiting   State = "waiting"
	Blocked   State = "blocked"
	Canceled  State = "canceled"
	Uncertain State = "uncertain"
)

type Reason string

const (
	NoReason      Reason = ""
	Error         Reason = "error"
	Deadline      Reason = "deadline"
	Cancellation  Reason = "cancellation"
	ConditionGate Reason = "condition-not-met"
)

// Scope is logical correlation, never backend authority or principal identity.
type Scope struct{ Deployment, Instance, Target string }

// Event is a core-owned transition. Step/operation IDs are run-local opaque IDs,
// not arbitrary plan names or backend resource names. Attempt is one-based for
// actual check attempts; a waiting condition references its last attempt. A
// condition's terminal event has Attempt zero and may retain the last observation
// time: a timeout during a wait is not a new observation. AcceptedRevision is a
// host-bound nonsecret expectation, not accepted-state promotion.
// No raw error, configuration, diagnostic text, secret reference or guest output
// belongs here. Typed metadata is not a sandbox or a secret-redaction mechanism.
type Event struct {
	SchemaVersion    string
	RunID            string
	Sequence         uint64
	Time             time.Time
	Kind             Kind
	State            State
	Reason           Reason
	StepID           string
	OperationID      string
	Scope            Scope
	Phase            string
	CheckID          string
	Attempt          int
	Required         bool
	AcceptedRevision string
	ObservedAt       time.Time
}

// Report retains a bounded event prefix and the terminal run event. Dropped
// counts omitted transitions, not omitted execution. Sequence gaps are explicit;
// sequences have no ordering meaning across independent runs. This ephemeral
// report is neither a current-status snapshot nor a durable operation journal.
type Report struct {
	RunID   string
	Events  []Event
	Dropped uint64
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// Validate checks envelope shape and closed vocabulary, not lifecycle ordering
// or the truth/authorization of observations. Producers own nonsecret metadata.
func (e Event) Validate() error {
	if e.SchemaVersion != SchemaVersion || !identifier.MatchString(e.RunID) || e.Sequence == 0 || e.Time.IsZero() {
		return fmt.Errorf("invalid event envelope")
	}
	switch e.State {
	case Running, Completed, Simulated, Passed, Failed, Unknown, Skipped, Waiting, Blocked, Canceled, Uncertain:
	default:
		return fmt.Errorf("invalid event state")
	}
	switch e.Reason {
	case NoReason, Error, Deadline, Cancellation, ConditionGate:
	default:
		return fmt.Errorf("invalid event reason")
	}
	if len(e.AcceptedRevision) > 2048 || strings.ContainsFunc(e.AcceptedRevision, unicode.IsControl) {
		return fmt.Errorf("invalid event revision")
	}
	if e.Scope != (Scope{}) && (!identifier.MatchString(e.Scope.Deployment) || !identifier.MatchString(e.Scope.Instance) || !identifier.MatchString(e.Scope.Target)) {
		return fmt.Errorf("invalid event scope")
	}
	switch e.Kind {
	case Run, Validation, Authorization, Preflight:
		if e.StepID != "" || e.OperationID != "" || e.Scope != (Scope{}) {
			return fmt.Errorf("run phase cannot claim step scope")
		}
	case Step, Operation, Condition, CheckAttempt:
		if !identifier.MatchString(e.StepID) || !identifier.MatchString(e.OperationID) {
			return fmt.Errorf("step event requires correlation IDs")
		}
	default:
		return fmt.Errorf("invalid event kind")
	}
	allowed := false
	switch e.Kind {
	case Run, Step:
		allowed = e.State == Running || e.State == Completed || e.State == Simulated || e.State == Failed || e.State == Blocked || e.State == Canceled || e.State == Uncertain
	case Validation, Authorization, Preflight:
		allowed = e.State == Running || e.State == Completed || e.State == Failed || e.State == Blocked || e.State == Canceled
	case Operation:
		allowed = e.State == Running || e.State == Completed || e.State == Simulated || e.State == Failed || e.State == Canceled || e.State == Uncertain
	case Condition:
		allowed = e.State == Running || e.State == Passed || e.State == Failed || e.State == Unknown || e.State == Skipped || e.State == Waiting || e.State == Canceled
	case CheckAttempt:
		allowed = e.State == Running || e.State == Passed || e.State == Failed || e.State == Unknown || e.State == Skipped || e.State == Canceled
	}
	if !allowed {
		return fmt.Errorf("invalid state for event kind")
	}
	if e.Kind == Condition || e.Kind == CheckAttempt {
		if e.Scope == (Scope{}) || !identifier.MatchString(e.CheckID) || e.Attempt < 0 ||
			(e.Kind == CheckAttempt && e.Attempt == 0) ||
			(e.Phase != "preflight" && e.Phase != "precondition" && e.Phase != "postcondition") {
			return fmt.Errorf("invalid check correlation")
		}
	} else if e.CheckID != "" || e.Phase != "" || e.Attempt != 0 || e.Required || e.AcceptedRevision != "" || !e.ObservedAt.IsZero() {
		return fmt.Errorf("non-check event cannot claim check evidence")
	}
	return nil
}
