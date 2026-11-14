package cli

import (
	"context"
	"encoding/json"

	operatorops "github.com/blevinn/aginctus/internal/cli/operations/operator"
)

type projectConfinementEvidence = operatorops.ProjectConfinementEvidence

const (
	projectConfinementUnknown projectConfinementEvidence = operatorops.ProjectConfinementUnknown
	projectConfinementUnrestricted projectConfinementEvidence = operatorops.ProjectConfinementUnrestricted
	projectConfinementShared projectConfinementEvidence = operatorops.ProjectConfinementShared
	projectConfinementNetwork projectConfinementEvidence = operatorops.ProjectConfinementNetwork
	projectConfinementCandidate projectConfinementEvidence = operatorops.ProjectConfinementCandidate
)

func classifyProjectConfinement(data json.RawMessage, expectedProject string) projectConfinementEvidence {
	return operatorops.ClassifyProjectConfinement(data, expectedProject)
}

func inspectProjectConfinement(ctx context.Context, project string) projectConfinementEvidence {
	return operatorops.InspectProjectConfinement(ctx, project, readOperatorAPI)
}
