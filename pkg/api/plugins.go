package api

// PluginCapability is a compiled capability declaration, not a runtime
// instance or permission grant.
type PluginCapability struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// Plugin is a public, nonsecret projection of a compiled plugin descriptor.
// It deliberately does not expose runtime configuration or implementation
// interfaces.
type Plugin struct {
	Name         string             `json:"name"`
	Version      string             `json:"version"`
	APIVersion   string             `json:"pluginApiVersion"`
	Capabilities []PluginCapability `json:"capabilities"`
	Dependencies []string           `json:"dependencies"`
}

// PluginList is a typed, environment-independent compiled plugin inventory.
type PluginList struct {
	TypeMeta
	Items []Plugin `json:"items"`
}
