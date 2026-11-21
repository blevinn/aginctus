package api

// SystemStatus is a nonsecret observation of the connected Incus daemon.
// Successful version retrieval is not proof of project authorization,
// managed-environment health, or convergence.
type SystemStatus struct {
	TypeMeta
	IncusVersion string `json:"incusVersion"`
	Scope string `json:"scope"`
}
