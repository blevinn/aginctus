package environment

import (
	"encoding/json"
	"errors"
	"slices"
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
	case "gateway-deliver", "gateway-provision", "gateway-start", "gateway-stop", "gateway-inspect":
		return g.WorkloadID == "gateway-credentials"
	case "herdr-reconcile", "herdr-bootstrap", "herdr-stop", "herdr-remove", "herdr-inspect":
		return g.WorkloadID == "herdr-client"
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
	case "gateway-provision":
		switch e.Action {
		case "image-postgres", "image-litellm", "alias-postgres", "alias-litellm", "volume-database", "instance-postgres", "instance-litellm":
			return true
		}
		return false
	case "gateway-stop":
		return e.Action == "stop-litellm" || e.Action == "stop-postgres"
	case "gateway-start":
		return e.Action == "configure-postgres" || e.Action == "configure-litellm" || e.Action == "start-postgres" || e.Action == "start-litellm"
	case "gateway-deliver":
		return e.Action == "deliver"
	case "herdr-bootstrap":
		return e.Action == "create-state" || e.Action == "create" || e.Action == "start"
	case "herdr-stop":
		return e.Action == "stop"
	case "herdr-remove":
		return e.Action == "delete"
	case "herdr-reconcile":
		return e.Action == "create" || e.Action == "start"
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
	return g.Operation == "reconcile" || g.Operation == "stop" || g.Operation == "remove" || g.Operation == "gateway-deliver" || g.Operation == "gateway-provision" || g.Operation == "gateway-start" || g.Operation == "gateway-stop" || g.Operation == "herdr-reconcile" || g.Operation == "herdr-bootstrap" || g.Operation == "herdr-stop" || g.Operation == "herdr-remove"
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

func (g projectExecutionGrant) domainSelected(c Candidate) bool {
	switch g.Operation {
	case "gateway-deliver", "gateway-provision", "gateway-start", "gateway-stop", "gateway-inspect":
		return g.WorkloadID == "gateway-credentials" && c.Gateway != nil
	case "herdr-bootstrap":
		return g.WorkloadID == "herdr-client" && c.Herdr != nil && c.Herdr.AllowStateBootstrap
	case "herdr-reconcile", "herdr-stop", "herdr-remove", "herdr-inspect":
		return g.WorkloadID == "herdr-client" && c.Herdr != nil
	default:
		return slices.Contains(c.SelectedWorkloads, g.WorkloadID)
	}
}

func (g projectExecutionGrant) domainScope() bool {
	return g.Operation == "gateway-deliver" || g.Operation == "gateway-provision" || g.Operation == "gateway-start" || g.Operation == "gateway-stop" || g.Operation == "herdr-reconcile" || g.Operation == "herdr-bootstrap" || g.Operation == "herdr-stop" || g.Operation == "herdr-remove"
}

func (g projectExecutionGrant) inspection() bool {
	return g.Operation == "inspect" || g.Operation == "gateway-inspect" || g.Operation == "herdr-inspect"
}

func (g projectExecutionGrant) inspects(other projectExecutionGrant) bool {
	if g.WorkloadID != other.WorkloadID {
		return false
	}
	switch g.Operation {
	case "gateway-inspect":
		return other.Operation == "gateway-deliver" || other.Operation == "gateway-provision" || other.Operation == "gateway-start" || other.Operation == "gateway-stop"
	case "herdr-inspect":
		return other.Operation == "herdr-reconcile" || other.Operation == "herdr-bootstrap" || other.Operation == "herdr-stop" || other.Operation == "herdr-remove"
	case "inspect":
		return !other.domainScope()
	default:
		return false
	}
}

func (g projectExecutionGrant) synchronous(action string) bool {
	return g.Operation == "gateway-deliver" && action == "deliver" ||
		g.Operation == "gateway-provision" && (action == "alias-postgres" || action == "alias-litellm" || action == "volume-database") ||
		g.Operation == "herdr-bootstrap" && action == "create-state"
}
