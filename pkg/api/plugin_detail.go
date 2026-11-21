package api

// PluginDetail is a typed read-only projection of one compiled implementation.
// The embedded Plugin fields appear at the document root; there is no generic
// data/result wrapper and no runtime configuration or credentials.
type PluginDetail struct {
	TypeMeta
	Plugin
}
