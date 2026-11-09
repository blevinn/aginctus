package cli

import (
	"context"
	"encoding/json"
	"net/url"
)

// projectConfinementEvidence reports only observed project policy shape.
// It never proves the caller's effective permission to write. A local Unix
// Incus administrator remains an administrator even when the selected project
// looks restricted.
type projectConfinementEvidence string

const (
	projectConfinementUnknown      projectConfinementEvidence = "unverified"
	projectConfinementUnrestricted projectConfinementEvidence = "not-restricted"
	projectConfinementShared       projectConfinementEvidence = "shared-resource-namespace"
	projectConfinementNetwork      projectConfinementEvidence = "shared-network-namespace"
	projectConfinementCandidate    projectConfinementEvidence = "restricted-isolated-candidate"
)

// classifyProjectConfinement refuses to infer a safe delegation model from
// absent/defaulted feature flags. In particular, features.networks=false
// inherits host-shared networking and must NOT be treated as confinement.
func classifyProjectConfinement(data json.RawMessage, expectedProject string) projectConfinementEvidence {
	var response struct {
		Name   string            `json:"name"`
		Config map[string]string `json:"config"`
	}
	if json.Unmarshal(data, &response) != nil ||
		response.Name != expectedProject || response.Config == nil {
		return projectConfinementUnknown
	}
	switch response.Config["restricted"] {
	case "false":
		return projectConfinementUnrestricted
	case "true":
		// Continue only after explicit restriction evidence.
	default:
		return projectConfinementUnknown
	}
	for _, feature := range []string{"features.images", "features.profiles", "features.storage.volumes"} {
		switch response.Config[feature] {
		case "true":
		case "false":
			return projectConfinementShared
		default:
			return projectConfinementUnknown
		}
	}
	switch response.Config["features.networks"] {
	case "false":
		return projectConfinementNetwork
	case "true":
		return projectConfinementCandidate
	default:
		return projectConfinementUnknown
	}
}

// inspectProjectConfinement makes one GET against the explicit selected
// project. It does not use a caller-configured project default or a write
// probe, and a failed/denied read is never converted into "safe".
func inspectProjectConfinement(ctx context.Context, project string) projectConfinementEvidence {
	data, status := readOperatorAPI(ctx, "/1.0/projects/"+url.PathEscape(project))
	if status != operatorReadOK {
		return projectConfinementUnknown
	}
	return classifyProjectConfinement(data, project)
}
