package environment

import (
	"encoding/json"
	"errors"
)

const projectExecutionPolicyVersion = "aginctus.dev/project-execution-policy/v1alpha1"

var errProjectExecutionDenied = errors.New("project execution denied")

// projectExecutionGrant is deliberately exact and workload-scoped. Neither a
// promoter grant nor an Incus project membership is an execution grant. Stop
// and remove are separately granted; granting reconcile does not grant either.
type projectExecutionGrant struct {
	PeerSHA256    string `json:"peerSha256"`
	ServerID      string `json:"serverId"`
	Project       string `json:"project"`
	EnvironmentID string `json:"environmentId"`
	RevisionID    string `json:"revisionId"`
	WorkloadID    string `json:"workloadId"`
	Operation     string `json:"operation"`
}

func (g projectExecutionGrant) valid(binding Binding) bool {
	if !validAcceptedHash(g.PeerSHA256) || !validAcceptedHash(g.RevisionID) ||
		!idPattern.MatchString(g.EnvironmentID) || !journalIdentifier.MatchString(g.WorkloadID) ||
		g.ServerID != binding.ServerID || g.Project != binding.Project {
		return false
	}
	switch g.Operation {
	case "reconcile", "stop", "remove", "recover", "inspect":
		return true
	default:
		return false
	}
}

func (g projectExecutionGrant) permits(e LocalNativeEffectRecord) bool {
	if e.Resource != g.WorkloadID || e.Target != g.WorkloadID {
		return false
	}
	switch g.Operation {
	case "reconcile":
		return e.Action == "create" || e.Action == "start"
	case "stop":
		return e.Action == "stop"
	case "remove":
		return e.Action == "delete"
	default:
		return false
	}
}

func (g projectExecutionGrant) writer() bool {
	return g.Operation == "reconcile" || g.Operation == "stop" || g.Operation == "remove"
}

func readProjectExecutionPolicy(path string, binding Binding) (map[projectExecutionGrant]struct{}, error) {
	data, err := readAdministratorPolicy(path)
	if err != nil {
		return nil, errProjectExecutionDenied
	}
	return parseProjectExecutionPolicy(data, binding)
}

func parseProjectExecutionPolicy(data []byte, binding Binding) (map[projectExecutionGrant]struct{}, error) {
	var fields map[string]json.RawMessage
	var version string
	var entries []json.RawMessage
	if rejectCandidateDuplicateKeys(data) != nil || json.Unmarshal(data, &fields) != nil ||
		candidateKeys(fields, "apiVersion", "grants") != nil ||
		json.Unmarshal(fields["apiVersion"], &version) != nil || version != projectExecutionPolicyVersion ||
		json.Unmarshal(fields["grants"], &entries) != nil || entries == nil {
		return nil, errProjectExecutionDenied
	}
	grants := make(map[projectExecutionGrant]struct{}, len(entries))
	for _, entry := range entries {
		var fields map[string]json.RawMessage
		var grant projectExecutionGrant
		if json.Unmarshal(entry, &fields) != nil || candidateKeys(fields,
			"peerSha256", "serverId", "project", "environmentId", "revisionId", "workloadId", "operation") != nil ||
			json.Unmarshal(entry, &grant) != nil || !grant.valid(binding) {
			return nil, errProjectExecutionDenied
		}
		if _, duplicate := grants[grant]; duplicate {
			return nil, errProjectExecutionDenied
		}
		grants[grant] = struct{}{}
	}
	return grants, nil
}
