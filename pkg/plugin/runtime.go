package plugin

import (
	"context"

	"github.com/blevinn/aginctus/pkg/orchestration"
)

// CheckEvaluator is an optional runtime extension. The host validates and binds
// the selected declaration before invoking it under independent effect policy.
// Implementations must honor cancellation, emit safe errors/nonsecret findings,
// and finish all service calls before returning. Services are invocation-local;
// retaining them does not extend their lifetime or authority. Trusted Go plugins
// are not sandboxed by this interface.
type CheckEvaluator interface {
	Plugin
	EvaluateCheck(context.Context, CheckRequest, CheckServices) (orchestration.Finding, error)
}

// CheckRequest is a detached declaration snapshot, not accepted state or runtime
// credentials. IDs remain local; the host retains deployment/instance scope.
type CheckRequest struct {
	Target orchestration.Target
	Check  orchestration.Check
}

// CheckServices supplies only explicitly bound capabilities. Nil means absent,
// never ambient fallback. More service families require separate contracts.
type CheckServices struct {
	Containers ContainerInspector
}

// ContainerInspector reads only a declared local container. The host binds scope,
// authorization and the evaluation context; the plugin cannot replace that
// context with a longer-lived one. Calls after evaluation return fail closed.
// Inspection does not execute guest commands, expose raw configuration/secrets,
// create resources or establish application readiness.
type ContainerInspector interface {
	Inspect(containerID string) (orchestration.ContainerObservation, error)
}
