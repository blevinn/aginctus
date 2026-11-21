package api

import (
	"errors"
	"fmt"
)

// LifecycleChange describes the kind of change, not a privileged Incus
// operation. Plans must be nonsecret and remain advisory until the executor
// revalidates the current revision, ownership and authorization.
type LifecycleChange struct {
	Resource string `json:"resource"`
	Action   string `json:"action"`
	Reason   string `json:"reason,omitempty"`
}

// PlanBase is shared *data* for individually typed lifecycle plan documents.
// It has no authorization token, claim of executable permissions, or signature.
type PlanBase struct {
	Project       string `json:"project"`
	EnvironmentID string `json:"environmentId"`
	BaseRevision  string `json:"baseRevision,omitempty"`
	Changes       []LifecycleChange `json:"changes"`
}

// AcceptancePlan compares configured intent to the currently accepted
// revision. The candidate digest identifies previewed content, not approval.
type AcceptancePlan struct {
	TypeMeta
	PlanBase
	CandidateDigest string `json:"candidateDigest"`
}

// ReconciliationPlan previews accepted-to-observed changes. The pinned
// accepted revision is mandatory before any execution can be requested.
type ReconciliationPlan struct {
	TypeMeta
	PlanBase
	AcceptedRevision string `json:"acceptedRevision"`
}

// RemovalPlan can describe only a specifically named resource whose absence
// from configured state has been established. It does not grant deletion.
type RemovalPlan struct {
	TypeMeta
	PlanBase
	ResourceName     string `json:"resourceName"`
	AbsentConfigured bool   `json:"absentConfigured"`
}

// AcceptanceResult reports an *accepted-state CAS commit*; it does not claim
// that the newly accepted resource has converged in Incus.
type AcceptanceResult struct {
	TypeMeta
	Project          string                   `json:"project"`
	EnvironmentID    string                   `json:"environmentId"`
	AcceptedRevision AcceptedRevisionIdentity `json:"acceptedRevision"`
}

// ReconciliationStage separates acknowledgement, completed attempts and
// verified convergence. It must not be inferred from a successful API call.
type ReconciliationStage string

const (
	ReconciliationAcknowledged ReconciliationStage = "acknowledged"
	ReconciliationCompleted    ReconciliationStage = "completed"
	ReconciliationVerified     ReconciliationStage = "verified"
)

// ReconciliationResult represents evidence returned by a controller. A
// completed attempt can still report failures; verified requires convergence
// evidence. Revision pinning is mandatory for all stages.
type ReconciliationResult struct {
	TypeMeta
	Project          string              `json:"project"`
	EnvironmentID    string              `json:"environmentId"`
	AcceptedRevision string              `json:"acceptedRevision"`
	Stage            ReconciliationStage `json:"stage"`
	AttemptID        string              `json:"attemptId,omitempty"`
	Converged        bool                `json:"converged"`
}

// RemovalResult records a completed resource removal and the accepted
// revision used to authorize it. No result is a substitute for the executor's
// independent ownership and configured-state absence checks.
type RemovalResult struct {
	TypeMeta
	Project          string `json:"project"`
	EnvironmentID    string `json:"environmentId"`
	AcceptedRevision string `json:"acceptedRevision"`
	ResourceName     string `json:"resourceName"`
	Removed          bool   `json:"removed"`
}

func validateLifecycleScope(project, environment string) error {
	if project == "" || environment == "" {
		return errors.New("explicit project and immutable environment identity required")
	}
	return nil
}

func validatePlan(meta TypeMeta, kind string, base PlanBase) error {
	if meta != NewTypeMeta(kind) { return fmt.Errorf("invalid %s envelope",kind) }
	if err := validateLifecycleScope(base.Project, base.EnvironmentID); err != nil { return err }
	if base.Changes == nil { return errors.New("plan changes must be an array") }
	for _, change := range base.Changes {
		if change.Resource == "" || change.Action == "" {
			return errors.New("invalid plan resource or action")
		}
	}
	return nil
}

func (p AcceptancePlan) Validate() error {
	if err := validatePlan(p.TypeMeta, "AcceptancePlan", p.PlanBase); err != nil { return err }
	if p.CandidateDigest == "" { return errors.New("acceptance plan requires a candidate digest") }
	return nil
}
func (p ReconciliationPlan) Validate() error {
	if err := validatePlan(p.TypeMeta, "ReconciliationPlan", p.PlanBase); err != nil { return err }
	if p.AcceptedRevision == "" || p.BaseRevision != "" && p.BaseRevision != p.AcceptedRevision {
		return errors.New("reconciliation requires one pinned accepted revision")
	}
	return nil
}
func (p RemovalPlan) Validate() error {
	if err := validatePlan(p.TypeMeta, "RemovalPlan", p.PlanBase); err != nil { return err }
	if p.ResourceName == "" || !p.AbsentConfigured {
		return errors.New("removal plan requires proven configured-state absence")
	}
	if len(p.Changes) != 1 || p.Changes[0].Resource != p.ResourceName || p.Changes[0].Action != "remove" {
		return errors.New("removal plan must address exactly one named resource")
	}
	return nil
}
func (r AcceptanceResult) Validate() error {
	if r.TypeMeta != NewTypeMeta("AcceptanceResult") { return errors.New("invalid acceptance result envelope") }
	if err := validateLifecycleScope(r.Project, r.EnvironmentID); err != nil { return err }
	if r.AcceptedRevision.ID == "" || r.AcceptedRevision.Generation == 0 || r.AcceptedRevision.CandidateDigest == "" {
		return errors.New("acceptance requires a committed revision identity")
	}
	return nil
}
func (r ReconciliationResult) Validate() error {
	if r.TypeMeta != NewTypeMeta("ReconciliationResult") { return errors.New("invalid reconciliation result envelope") }
	if err := validateLifecycleScope(r.Project,r.EnvironmentID); err != nil { return err }
	if r.AcceptedRevision == "" { return errors.New("reconciliation result requires pinned accepted revision") }
	switch r.Stage {
	case ReconciliationAcknowledged:
		if r.Converged || r.AttemptID != "" { return errors.New("acknowledgement is not a completed attempt") }
	case ReconciliationCompleted:
		if r.Converged || r.AttemptID == "" { return errors.New("completed attempt does not establish convergence") }
	case ReconciliationVerified:
		if !r.Converged || r.AttemptID == "" { return errors.New("verified convergence requires attempt evidence") }
	default:
		return errors.New("invalid reconciliation stage")
	}
	return nil
}
func (r RemovalResult) Validate() error {
	if r.TypeMeta != NewTypeMeta("RemovalResult") { return errors.New("invalid removal result envelope") }
	if err := validateLifecycleScope(r.Project,r.EnvironmentID); err != nil { return err }
	if r.AcceptedRevision == "" || r.ResourceName == "" || !r.Removed {
		return errors.New("completed removal requires accepted revision and resource evidence")
	}
	return nil
}
