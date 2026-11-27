package environment

import (
	"encoding/json"
	"strings"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// ProtectedGatewayGrant is an independent administrator policy for one exact
// accepted revision. The digest commits to the entire gateway intent (images,
// networks, pools, version); the physical project is never candidate input.
type ProtectedGatewayGrant struct {
	APIVersion string `json:"apiVersion"`
	ServerID string `json:"serverId"`
	Project string `json:"project"`
	EnvironmentID string `json:"environmentId"`
	RevisionID string `json:"revisionId"`
	ProtectedProject string `json:"protectedProject"`
}

func (g ProtectedGatewayGrant) valid() bool {
	pin, ok := strings.CutPrefix(g.ServerID, "sha256:")
	_, logicalErr := incusscope.NewProjectScope(g.Project)
	_, physicalErr := incusscope.NewProjectScope(g.ProtectedProject)
	return g.APIVersion == "aginctus.dev/protected-gateway-grant/v1alpha1" &&
		ok && validAcceptedHash(pin) && logicalErr == nil && physicalErr == nil &&
		g.Project != g.ProtectedProject && idPattern.MatchString(g.EnvironmentID) && validAcceptedHash(g.RevisionID)
}

func parseProtectedGatewayGrant(data []byte) (ProtectedGatewayGrant, error) {
	var g ProtectedGatewayGrant
	var fields map[string]json.RawMessage
	if rejectCandidateDuplicateKeys(data) != nil || json.Unmarshal(data, &fields) != nil ||
		candidateKeys(fields, "apiVersion", "serverId", "project", "environmentId", "revisionId", "protectedProject") != nil ||
		json.Unmarshal(data, &g) != nil || !g.valid() {
		return ProtectedGatewayGrant{}, errProjectExecutionDenied
	}
	return g, nil
}

func ReadProtectedGatewayGrant(path string) (ProtectedGatewayGrant, error) {
	data, err := readAdministratorPolicy(path)
	if err != nil {
		return ProtectedGatewayGrant{}, errProjectExecutionDenied
	}
	return parseProtectedGatewayGrant(data)
}

// VerifyCandidate never rewrites the logical binding to the physical project.
func (g ProtectedGatewayGrant) VerifyCandidate(r AcceptedRevision, c Candidate) error {
	if !g.valid() || r.ValidateAgainst(c) != nil || c.Gateway == nil ||
		r.ServerID != g.ServerID || r.Project != g.Project || r.EnvironmentID != g.EnvironmentID || r.ID != g.RevisionID ||
		c.Gateway.Postgres.IncusFingerprint == c.Gateway.LiteLLM.IncusFingerprint {
		return errProjectExecutionDenied
	}
	return nil
}
