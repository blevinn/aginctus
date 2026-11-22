package environment

import (
	"context"
	"errors"
	"slices"
)

// LocalRecoveryInventory is a nonsecret list of independently validated
// published environment heads in one SAME-UID project-volume fixture. It is
// not a backup, snapshot transaction or authenticated project assertion.
type LocalRecoveryInventory struct {
	ServerID     string              `json:"serverId"`
	Project      string              `json:"project"`
	Environments []LocalHistoryAudit `json:"environments"`
}

// InspectLocalRecoveryInventory verifies only an explicitly enumerated set of
// enrolled environments; it does NOT discover complete project enrollment.
// A real backup requires external writer quiescence and an atomic volume
// snapshot: two matching passes do not guarantee an intervening write did not
// occur, and must not be treated as proof of snapshot consistency.
//
// This method neither modifies state nor follows arbitrary paths supplied by
// candidate files. It refuses duplicate UUIDs and mixed server/project scopes.
func (s *LocalProjectStateStore) InspectLocalRecoveryInventory(
	ctx context.Context, enrolled []Descriptor,
) (LocalRecoveryInventory, error) {
	if ctx == nil {
		return LocalRecoveryInventory{}, errors.New("local recovery inspection requires context")
	}
	if err := ctx.Err(); err != nil {
		return LocalRecoveryInventory{}, err
	}
	if s == nil || len(enrolled) == 0 || len(enrolled) > 256 {
		return LocalRecoveryInventory{}, errors.New("explicit bounded environment inventory required")
	}
	descriptors := slices.Clone(enrolled)
	slices.SortFunc(descriptors, func(a, b Descriptor) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	out := LocalRecoveryInventory{ServerID: s.binding.ServerID, Project: s.binding.Project,
		Environments: make([]LocalHistoryAudit, 0, len(descriptors))}
	for i, d := range descriptors {
		if i > 0 && descriptors[i-1].ID == d.ID {
			return LocalRecoveryInventory{}, errors.New("duplicate environment in recovery inspection")
		}
		if d.Validate() != nil || d.Binding != s.binding {
			return LocalRecoveryInventory{}, errors.New("foreign or invalid recovery environment")
		}
		h, err := s.AuditLocalPublishedHistory(ctx, d)
		if err != nil {
			return LocalRecoveryInventory{}, errors.New("unverifiable published environment history")
		}
		out.Environments = append(out.Environments, h)
	}
	// A changed head invalidates the inventory. This only detects changes
	// observed between passes; external volume quiescence is still required
	// for real backup and restore.
	for i, d := range descriptors {
		h, err := s.AuditLocalPublishedHistory(ctx, d)
		if err != nil || h != out.Environments[i] {
			return LocalRecoveryInventory{}, errors.New("project recovery inventory changed during inspection")
		}
	}
	if err := ctx.Err(); err != nil {
		return LocalRecoveryInventory{}, err
	}
	return out, nil
}
