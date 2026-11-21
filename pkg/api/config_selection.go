package api

// ConfigSelection is a nonsecret projection of effective CLI preferences.
// It is NOT authenticated Incus authority, an enrolled environment, or
// accepted-state intent. No credential or arbitrary configuration values
// belong in this public response.
type ConfigSelection struct {
	TypeMeta
	IncusProject string `json:"incusProject"`
	Authority string `json:"authority"`
}
