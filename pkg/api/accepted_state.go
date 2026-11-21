package api

// AcceptedRevisionIdentity is a nonsecret projection of one published,
// immutable revision. A content hash is not a signature or authorization.
type AcceptedRevisionIdentity struct {
	ID              string `json:"id"`
	Generation      uint64 `json:"generation"`
	ParentID        string `json:"parentId"`
	CandidateDigest string `json:"candidateDigest"`
}

// AcceptedStateStatus is an authenticated read projection for exactly one
// environment in an Incus project. Absent accepted state is NOT an error;
// an unavailable or unauthorized backend MUST return an error instead.
type AcceptedStateStatus struct {
	TypeMeta
	Environment Environment               `json:"environment"`
	Layer       string                    `json:"layer"`
	HasAccepted bool                      `json:"hasAccepted"`
	Revision    *AcceptedRevisionIdentity `json:"revision,omitempty"`
}

// ConfiguredAcceptedDiff reports only nonsecret field names and workload IDs.
// It is read-only; an unselected workload is NOT permission to delete it.
type ConfiguredAcceptedDiff struct {
	TypeMeta
	Environment        Environment               `json:"environment"`
	FromLayer          string                    `json:"fromLayer"`
	ToLayer            string                    `json:"toLayer"`
	HasAccepted        bool                      `json:"hasAccepted"`
	Revision           *AcceptedRevisionIdentity `json:"revision,omitempty"`
	ProposedDigest     string                    `json:"proposedDigest"`
	Changed            bool                      `json:"changed"`
	DisplayNameChanged bool                      `json:"displayNameChanged"`
	NewlySelected      []string                  `json:"newlySelected"`
	Unselected         []string                  `json:"unselected"`
	Modified           []AcceptedWorkloadChange  `json:"modified"`
}

// AcceptedWorkloadChange contains field NAMES, never old/new field values.
type AcceptedWorkloadChange struct {
	ID     string   `json:"id"`
	Fields []string `json:"fields"`
}
