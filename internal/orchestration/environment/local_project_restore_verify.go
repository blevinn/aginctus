package environment

import (
	"context"
	"errors"
	"os"
)

// LocalRestoreVerification summarizes a read-only comparison of the published
// heads and full reachable lineages across two independent same-UID stores.
// It is not proof of a valid project-volume snapshot, a complete backup or
// authenticated Incus project authority.
type LocalRestoreVerification struct {
	ServerID     string              `json:"serverId"`
	Project      string              `json:"project"`
	Environments []LocalHistoryAudit `json:"environments"`
}

// CompareLocalRestoredProject checks an explicitly selected, independently
// opened source and restored fixture. It never copies files, changes either
// store, discovers enrollment or contacts Incus. The caller MUST prove that
// the source was quiesced and the restored volume independently provisioned;
// identical content alone does not establish ownership or snapshot atomicity.
func CompareLocalRestoredProject(
	ctx context.Context,
	source, restored *LocalProjectStateStore,
	enrolled []Descriptor,
) (LocalRestoreVerification, error) {
	if ctx == nil || source == nil || restored == nil {
		return LocalRestoreVerification{}, errors.New("two explicit project stores and context required")
	}
	if err := ctx.Err(); err != nil {
		return LocalRestoreVerification{}, err
	}
	if source.root == restored.root || source.binding != restored.binding {
		return LocalRestoreVerification{}, errors.New("restore must be distinct and have exact source project binding")
	}
	// Reject alternate paths to the same directory, even if source and
	// restore were opened with different names. Never compare one store to
	// itself and claim a successful restoration.
	leftInfo, err := os.Stat(source.root)
	if err != nil {
		return LocalRestoreVerification{}, errors.New("source root unavailable")
	}
	rightInfo, err := os.Stat(restored.root)
	if err != nil || os.SameFile(leftInfo, rightInfo) {
		return LocalRestoreVerification{}, errors.New("independent restore root unavailable")
	}
	left, err := source.InspectLocalRecoveryInventory(ctx, enrolled)
	if err != nil {
		return LocalRestoreVerification{}, errors.New("source recovery inventory unverified")
	}
	right, err := restored.InspectLocalRecoveryInventory(ctx, enrolled)
	if err != nil {
		return LocalRestoreVerification{}, errors.New("restored recovery inventory unverified")
	}
	if len(left.Environments) != len(right.Environments) ||
		left.ServerID != right.ServerID || left.Project != right.Project {
		return LocalRestoreVerification{}, errors.New("restored project inventory mismatch")
	}
	for i := range left.Environments {
		if left.Environments[i] != right.Environments[i] {
			return LocalRestoreVerification{}, errors.New("restored environment history differs")
		}
	}
	// Re-read source to detect some, not all, mid-comparison advances.
	again, err := source.InspectLocalRecoveryInventory(ctx, enrolled)
	if err != nil || len(again.Environments) != len(left.Environments) {
		return LocalRestoreVerification{}, errors.New("source changed during restore comparison")
	}
	for i := range left.Environments {
		if left.Environments[i] != again.Environments[i] {
			return LocalRestoreVerification{}, errors.New("source changed during restore comparison")
		}
	}
	if err := ctx.Err(); err != nil {
		return LocalRestoreVerification{}, err
	}
	return LocalRestoreVerification(left), nil
}
