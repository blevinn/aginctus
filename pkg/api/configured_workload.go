package api

// ConfiguredWorkload is an explicitly configured (not accepted or observed)
// workload candidate. Legacy pinned workload specifications do not yet carry
// a verified class, implementation or multi-container topology; this shape
// MUST NOT be silently upgraded into an authoritative Workload.
type ConfiguredWorkload struct {
	ID string `json:"id"`
	InstanceName string `json:"instanceName"`
	InstanceType string `json:"instanceType"`
	ImageFingerprint string `json:"imageFingerprint"`
	Profiles []string `json:"profiles"`
	EnsureRunning bool `json:"ensureRunning"`
}

// ConfiguredWorkloadList is a candidate-layer preview; it carries no accepted
// revision or Incus observation evidence.
type ConfiguredWorkloadList struct {
	TypeMeta
	EnvironmentID string `json:"environmentId"`
	Layer string `json:"layer"`
	Items []ConfiguredWorkload `json:"items"`
}
