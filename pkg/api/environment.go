package api

// EnvironmentBinding identifies an environment's immutable Incus authority
// boundary. ServerID is a verified server identity, never a connection alias.
// This public projection is evidence, not a permission grant.
type EnvironmentBinding struct {
	ServerID string `json:"serverId"`
	Project  string `json:"project"`
}

// Environment is the public identity projection for one independently owned
// lifecycle. ID and binding are immutable; Name may change.
type Environment struct {
	ID      string             `json:"id"`
	Name    string             `json:"name"`
	Binding EnvironmentBinding `json:"binding"`
}

// EnvironmentDetail is a versioned read-only document, not an accepted-state
// revision, candidate declaration, or grant of Incus access.
type EnvironmentDetail struct {
	TypeMeta
	Environment
}

// EnvironmentList enumerates independently identified environments. Items must
// be preverified by the owning reader, never inferred from instance labels.
type EnvironmentList struct {
	TypeMeta
	Items []Environment `json:"items"`
}

// EnvironmentSelectionRequest selects an enrolled immutable ID. Name alone is
// insufficient to identify an environment when many share one Incus project.
// The service must independently verify the authenticated server/project
// before presenting a matching environment.
type EnvironmentSelectionRequest struct {
	ID string
}
