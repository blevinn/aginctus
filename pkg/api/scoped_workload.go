package api

// ScopedWorkloadStatusRequest requests observation of configured legacy
// workloads inside an explicitly verified local Incus project. This is NOT
// an environment selector or a three-layer state query.
type ScopedWorkloadStatusRequest struct {
	Project     string
	Name        string
	RunningOnly bool
}

// ScopedWorkloadStatus is an observed legacy workload instance. No accepted
// revision, environment identity or convergence claim can be inferred.
type ScopedWorkloadStatus struct {
	Name     string `json:"name"`
	Instance string `json:"instance"`
	State    string `json:"state"`
}

// ScopedWorkloadStatusList is a read-only, versioned inventory of configured
// workload observations. Items are ordered by configured workload name.
type ScopedWorkloadStatusList struct {
	TypeMeta
	Project string                 `json:"project"`
	Items   []ScopedWorkloadStatus `json:"items"`
}
