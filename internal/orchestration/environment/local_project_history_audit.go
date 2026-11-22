package environment

import (
	"context"
	"errors"
	"fmt"
)

// LocalHistoryAudit describes only the published, reachable revision chain
// of one environment inside the same-UID project-volume fixture. It contains
// no candidate values and is NOT evidence of project authentication.
type LocalHistoryAudit struct {
	EnvironmentID string `json:"environmentId"`
	ServerID      string `json:"serverId"`
	Project       string `json:"project"`
	HasAccepted   bool   `json:"hasAccepted"`
	HeadID        string `json:"headId,omitempty"`
	Generation    uint64 `json:"generation"`
	Reachable     uint64 `json:"reachable"`
}

const maxLocalHistoryAuditRevisions = 8192

// AuditLocalPublishedHistory verifies every parent link from the published
// active pointer to genesis, including content digests and environment scope.
// Unpublished records are deliberately NOT interpreted as accepted revisions.
// This is read-only offline fixture verification: it does not enumerate other
// users, authenticate Incus or prove power-loss durability/backup integrity.
func (s *LocalProjectStateStore) AuditLocalPublishedHistory(ctx context.Context, enrolled Descriptor) (LocalHistoryAudit, error) {
	if ctx == nil {
		return LocalHistoryAudit{}, errors.New("local history audit requires context")
	}
	if err := ctx.Err(); err != nil {
		return LocalHistoryAudit{}, err
	}
	path, err := s.namespace(enrolled)
	if err != nil {
		return LocalHistoryAudit{}, err
	}
	out := LocalHistoryAudit{
		EnvironmentID: enrolled.ID,
		ServerID: enrolled.Binding.ServerID,
		Project: enrolled.Binding.Project,
	}
	active, _, err := s.readNamespace(ctx, enrolled)
	if err != nil {
		return LocalHistoryAudit{}, err
	}
	if active == nil {
		return out, nil
	}
	if active.Generation > maxLocalHistoryAuditRevisions {
		return LocalHistoryAudit{}, errors.New("published history exceeds bounded audit limit")
	}
	out.HasAccepted = true
	out.HeadID = active.ID
	out.Generation = active.Generation
	current := *active
	for {
		if err := ctx.Err(); err != nil {
			return LocalHistoryAudit{}, err
		}
		revision, candidate, err := s.readRevision(path, current.ID)
		if err != nil {
			return LocalHistoryAudit{}, errors.New("published history has missing or corrupt revision")
		}
		if revision != current || revision.EnvironmentID != enrolled.ID ||
			revision.ServerID != enrolled.Binding.ServerID || revision.Project != enrolled.Binding.Project ||
			revision.ValidateAgainst(candidate) != nil {
			return LocalHistoryAudit{}, errors.New("published history has foreign or altered revision")
		}
		out.Reachable++
		if current.Generation == 1 {
			if current.ParentID != "" || out.Reachable != active.Generation {
				return LocalHistoryAudit{}, errors.New("published history does not reach valid genesis")
			}
			break
		}
		if !validAcceptedHash(current.ParentID) {
			return LocalHistoryAudit{}, errors.New("published history has invalid parent ID")
		}
		parent, _, err := s.readRevision(path, current.ParentID)
		if err != nil {
			return LocalHistoryAudit{}, errors.New("published history has missing or corrupt parent")
		}
		if parent.ID != current.ParentID || parent.Generation != current.Generation-1 ||
			parent.EnvironmentID != enrolled.ID || parent.ServerID != enrolled.Binding.ServerID ||
			parent.Project != enrolled.Binding.Project {
			return LocalHistoryAudit{}, fmt.Errorf("published history parent linkage invalid at generation %d", current.Generation)
		}
		current = parent
	}
	return out, nil
}
