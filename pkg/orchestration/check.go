package orchestration

import (
	"fmt"
	"time"
)

// CheckEffect distinguishes pure computation, inspection and active probes.
// Zero/unknown effects are invalid; a read can still require credentials/network
// access. Effects and access declarations describe requirements, not permission.
type CheckEffect string

const (
	Offline     CheckEffect = "offline"
	ReadOnly    CheckEffect = "read-only"
	ActiveProbe CheckEffect = "active-probe"
)

// Access identifies a resource class needed by a check. Scoped service bindings
// and authorization are host responsibilities, not implied by these labels.
type Access string

const (
	HostAccess       Access = "host"
	IncusAccess      Access = "incus"
	GuestAccess      Access = "guest"
	NetworkAccess    Access = "network"
	CredentialAccess Access = "credentials"
	ModelAccess      Access = "live-model"
	ToolAccess       Access = "tool-execution"
)

// Check declares a criterion and its evidence scope. The host chooses when
// prerequisites are satisfiable and rechecks mutation-boundary conditions.
// Timeout bounds evaluation; RetryInterval zero requests one attempt, otherwise
// bounded readiness polling. MaxAge zero disallows cached findings. No polling,
// probing or cache is implemented by this declaration.
type Check struct {
	ID            string
	Claim         string
	Scope         string
	Required      bool
	Effect        CheckEffect
	Access        []Access
	OptIns        []string
	Timeout       time.Duration
	RetryInterval time.Duration
	MaxAge        time.Duration
}

// Validate checks metadata consistency without evaluating the check. Active
// probes require explicit opt-in names. A host must independently authorize all
// access, even if a plugin misclassifies its behavior or omits a requirement.
func (c Check) Validate() error {
	if !identifier.MatchString(c.ID) || !safeText(c.Claim, 1024) || !safeText(c.Scope, 1024) {
		return fmt.Errorf("check requires a valid ID, bounded claim and evidence scope")
	}
	if c.Timeout <= 0 || c.RetryInterval < 0 || c.RetryInterval >= c.Timeout || c.MaxAge < 0 {
		return fmt.Errorf("invalid check timeout, retry interval or freshness policy")
	}
	switch c.Effect {
	case Offline:
		if len(c.Access) != 0 || len(c.OptIns) != 0 {
			return fmt.Errorf("offline checks cannot require external access or opt-ins")
		}
	case ReadOnly, ActiveProbe:
		if len(c.Access) == 0 {
			return fmt.Errorf("non-offline checks must declare access requirements")
		}
		if c.Effect == ActiveProbe && len(c.OptIns) == 0 {
			return fmt.Errorf("active probes must declare explicit opt-ins")
		}
	default:
		return fmt.Errorf("unknown check effect")
	}
	accesses := make(map[Access]bool, len(c.Access))
	for _, access := range c.Access {
		switch access {
		case HostAccess, IncusAccess, GuestAccess, NetworkAccess, CredentialAccess:
		case ModelAccess, ToolAccess:
			if c.Effect != ActiveProbe {
				return fmt.Errorf("live-model and tool checks must be active probes")
			}
		default:
			return fmt.Errorf("unknown check access requirement")
		}
		if accesses[access] {
			return fmt.Errorf("duplicate check access requirement")
		}
		accesses[access] = true
	}
	optIns := make(map[string]bool, len(c.OptIns))
	for _, optIn := range c.OptIns {
		if !identifier.MatchString(optIn) || optIns[optIn] {
			return fmt.Errorf("invalid or duplicate check opt-in")
		}
		optIns[optIn] = true
	}
	return nil
}

// Outcome describes evidence, not operation success. Required failed, unknown
// and skipped findings cannot establish convergence. Zero is deliberately invalid.
type Outcome string

const (
	Passed  Outcome = "passed"
	Failed  Outcome = "failed"
	Unknown Outcome = "unknown"
	Skipped Outcome = "skipped"
)

// Finding is a bounded, nonsecret result, correlated with a local target/check
// pair. The host must carry deployment/instance scope in its execution envelope;
// these IDs are not globally unique.
// Code is a stable diagnostic identifier; messages must not contain raw errors,
// credentials, environment dumps or unrestricted guest output. Incarnation and
// revision identify evidence where applicable, not authority. Consumers must
// verify correlation, identity and freshness before using a finding.
type Finding struct {
	TargetID            string
	CheckID             string
	Outcome             Outcome
	ObservedAt          time.Time
	Code                string
	Message             string
	Remediation         string
	ResourceIncarnation string
	AcceptedRevision    string
}

// Validate rejects incomplete/invalid results. A valid finding, including a
// passed one, is not proof that a target has converged or that evidence is fresh.
func (f Finding) Validate() error {
	if !identifier.MatchString(f.TargetID) || !identifier.MatchString(f.CheckID) ||
		!identifier.MatchString(f.Code) || f.ObservedAt.IsZero() {
		return fmt.Errorf("finding requires target/check identity, diagnostic code and observation time")
	}
	switch f.Outcome {
	case Passed, Failed, Unknown, Skipped:
	default:
		return fmt.Errorf("unknown finding outcome")
	}
	for _, text := range []string{f.Message, f.Remediation, f.ResourceIncarnation, f.AcceptedRevision} {
		if text != "" && !safeText(text, 2048) {
			return fmt.Errorf("finding text must be bounded and free of control characters")
		}
	}
	return nil
}
