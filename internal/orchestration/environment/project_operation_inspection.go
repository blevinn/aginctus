package environment

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
)

// ProjectOperationSummary is journal evidence, not independently observed
// Incus state. The inspection endpoint does not expose the writer certificate.
type ProjectOperationSummary struct {
	ID         string                    `json:"id"`
	Operation  string                    `json:"operation"`
	Status     LocalNativeStatus         `json:"status"`
	Unresolved bool                      `json:"unresolved"`
	Effects    []LocalNativeEffectRecord `json:"effects,omitempty"`
}

const operationInspectionLimit = 1 << 20

func inspectionRevision(r AcceptedRevision) AcceptedRevision {
	return AcceptedRevision{ServerID: r.ServerID, Project: r.Project, EnvironmentID: r.EnvironmentID, ID: r.ID}
}

func (s *LocalProjectStateStore) inspectProjectOperations(ctx context.Context, desc Descriptor, grant projectExecutionGrant, id string) ([]byte, error) {
	_, candidate, err := s.ReadLocalRevision(ctx, desc, grant.RevisionID)
	if err != nil || !slices.Contains(candidate.SelectedWorkloads, grant.WorkloadID) {
		return nil, errProjectExecutionDenied
	}
	ids := []string{id}
	if id == "" {
		ids, err = s.ListLocalNativeOperationIDs(ctx, desc)
		if err != nil {
			return nil, errProjectExecutionDenied
		}
	}
	out := make([]ProjectOperationSummary, 0, len(ids))
	for _, current := range ids {
		op, err := s.ReadLocalNativeOperation(ctx, desc, current)
		if err != nil {
			return nil, errProjectExecutionDenied
		}
		if op.Execution == nil || op.Revision.ID != grant.RevisionID || op.Execution.WorkloadID != grant.WorkloadID {
			if id != "" {
				return nil, errProjectExecutionDenied
			}
			continue
		}
		entry := ProjectOperationSummary{ID: op.ID, Operation: op.Execution.Operation, Status: op.Status, Unresolved: op.unresolved()}
		if id != "" {
			entry.Effects = op.Effects
		}
		out = append(out, entry)
	}
	raw, err := json.Marshal(out)
	if err != nil || len(raw) > operationInspectionLimit {
		return nil, errProjectExecutionDenied
	}
	return raw, nil
}

// InspectProjectOperations reads one operation, or summaries in the exact
// authorized revision/workload scope when operationID is empty.
func InspectProjectOperations(ctx context.Context, config RestrictedOperatorReadConfig, project, environmentID, revisionID, workloadID, operationID string) ([]ProjectOperationSummary, error) {
	if !validAcceptedHash(revisionID) || !idPattern.MatchString(environmentID) || !journalIdentifier.MatchString(workloadID) ||
		(operationID != "" && !localEnvironmentAttemptID.MatchString(operationID)) {
		return nil, errProjectExecutionDenied
	}
	reads, closeReads, err := openProductionOperatorReads(ctx, config)
	if err != nil {
		return nil, err
	}
	defer closeReads()
	if reads.stateSession == nil {
		return nil, errProjectExecutionDenied
	}
	desc, err := reads.AcceptedSnapshot.Catalog.Get(ctx, project, environmentID)
	if err != nil {
		return nil, errProjectExecutionDenied
	}
	s, err := reads.stateSession.open(ctx, desc)
	if err != nil {
		return nil, err
	}
	binding, err := s.VerifiedBinding(ctx)
	if err != nil || binding != desc.Binding {
		return nil, errProjectExecutionDenied
	}
	raw, err := json.Marshal(projectExecutionRequest{APIVersion: projectExecutionWireVersion, Step: "inspect", Operation: "inspect", ID: operationID, WorkloadID: workloadID,
		Revision: AcceptedRevision{ServerID: binding.ServerID, Project: binding.Project, EnvironmentID: desc.ID, ID: revisionID}})
	if err != nil {
		return nil, errProjectExecutionDenied
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint+projectExecutionPath, bytes.NewReader(raw))
	if err != nil {
		return nil, errProjectExecutionDenied
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(req)
	if err != nil {
		return nil, errProjectExecutionDenied
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusConflict {
		return nil, ErrLocalNativeUnresolved
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/json" {
		return nil, errProjectExecutionDenied
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, operationInspectionLimit+1))
	var result []ProjectOperationSummary
	if err != nil || len(data) > operationInspectionLimit || json.Unmarshal(data, &result) != nil || result == nil ||
		(operationID != "" && (len(result) != 1 || result[0].ID != operationID)) {
		return nil, errProjectExecutionDenied
	}
	if canonical, err := json.Marshal(result); err != nil || !bytes.Equal(data, canonical) {
		return nil, errProjectExecutionDenied
	}
	if after, err := s.VerifiedBinding(ctx); err != nil || after != binding {
		return nil, errProjectExecutionDenied
	}
	return result, nil
}
