package operatorops

import (
	"context"
	"encoding/json"
	"net/url"
)

// ProjectConfinementEvidence reports only observed project policy shape.
// It never proves the caller's effective permission to write. A local Unix
// Incus administrator remains an administrator even when the selected project
// looks restricted.
type ProjectConfinementEvidence string

const (
	ProjectConfinementUnknown      ProjectConfinementEvidence = "unverified"
	ProjectConfinementUnrestricted ProjectConfinementEvidence = "not-restricted"
	ProjectConfinementShared       ProjectConfinementEvidence = "shared-resource-namespace"
	ProjectConfinementNetwork      ProjectConfinementEvidence = "shared-network-namespace"
	ProjectConfinementCandidate    ProjectConfinementEvidence = "restricted-isolated-candidate"
)

// ClassifyProjectConfinement refuses to infer a safe delegation model from
// absent/defaulted feature flags. In particular, features.networks=false
// inherits host-shared networking and must NOT be treated as confinement.
func ClassifyProjectConfinement(data json.RawMessage, expectedProject string) ProjectConfinementEvidence {
	var response struct {
		Name   string            `json:"name"`
		Config map[string]string `json:"config"`
	}
	if json.Unmarshal(data, &response) != nil ||
		response.Name != expectedProject || response.Config == nil {
		return ProjectConfinementUnknown
	}
	switch response.Config["restricted"] {
	case "false":
		return ProjectConfinementUnrestricted
	case "true":
		// Continue only after explicit restriction evidence.
	default:
		return ProjectConfinementUnknown
	}
	for _, feature := range []string{"features.images", "features.profiles", "features.storage.volumes"} {
		switch response.Config[feature] {
		case "true":
		case "false":
			return ProjectConfinementShared
		default:
			return ProjectConfinementUnknown
		}
	}
	switch response.Config["features.networks"] {
	case "false":
		return ProjectConfinementNetwork
	case "true":
		return ProjectConfinementCandidate
	default:
		return ProjectConfinementUnknown
	}
}

// inspectProjectConfinement makes one GET against the explicit selected
// project. It does not use a caller-configured project default or a write
// probe, and a failed/denied read is never converted into "safe".
func InspectProjectConfinement(ctx context.Context, project string, read func(context.Context, string) (json.RawMessage, ReadStatus)) ProjectConfinementEvidence {
	data, status := read(ctx, "/1.0/projects/"+url.PathEscape(project))
	if status != ReadOK {
		return ProjectConfinementUnknown
	}
	return ClassifyProjectConfinement(data, project)
}
