package api

// WorkloadClass identifies the logical service role, not a plugin name.
// Class identity must come from an authoritative environment-owned resource
// declaration. Do not infer it from legacy Incus instance role labels.
type WorkloadClass string

const (
	WorkloadClassAgent WorkloadClass = "agent"
	WorkloadClassFrontend WorkloadClass = "frontend"
	WorkloadClassGateway WorkloadClass = "gateway"
)

// WorkloadComponent is one named constituent of a logical workload. A
// workload is not constrained to a single container or Incus instance.
// Instance is diagnostic identity, not sufficient ownership evidence.
type WorkloadComponent struct {
	Name string `json:"name"`
	Instance string `json:"instance,omitempty"`
}

// Workload is a read-only public projection of one environment-owned logical
// service. Implementation is separate from its class (e.g. Herdr/frontend).
type Workload struct {
	Name string `json:"name"`
	EnvironmentID string `json:"environmentId"`
	Class WorkloadClass `json:"class"`
	Implementation string `json:"implementation"`
	Components []WorkloadComponent `json:"components"`
}

// WorkloadDetail is a versioned public resource document.
type WorkloadDetail struct {
	TypeMeta
	Workload
}

// WorkloadList collects owned logical resources for one immutable
// environment. It must not aggregate foreign or project-only instances.
type WorkloadList struct {
	TypeMeta
	EnvironmentID string `json:"environmentId"`
	Items []Workload `json:"items"`
}

// ComponentObservation represents a scoped observation, never a declaration
// or authority decision. No observation can silently rewrite accepted state.
type ComponentObservation struct {
	Name string `json:"name"`
	State string `json:"state"`
}

// WorkloadStatus explicitly separates observation from desired/accepted
// intent. AcceptedRevision may only be populated when an authoritative
// environment accepted-state reader supplied it.
type WorkloadStatus struct {
	TypeMeta
	EnvironmentID string `json:"environmentId"`
	Name string `json:"name"`
	AcceptedRevision string `json:"acceptedRevision,omitempty"`
	Observed []ComponentObservation `json:"observed"`
}

// ListWorkloadsRequest selects a verified immutable environment identity.
// Class is an optional filter applied *after* ownership verification.
type ListWorkloadsRequest struct {
	EnvironmentID string
	Class WorkloadClass
}

// GetWorkloadRequest addresses one logical resource in an environment.
type GetWorkloadRequest struct {
	EnvironmentID string
	Name string
}
