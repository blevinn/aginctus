// Package gatewayassets embeds the public, callback-only OpenCode Go bundle for
// delivery through inline Compose configs. It contains no credentials or routes.
package gatewayassets

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
)

const (
	ConfigPath = "/app/aginctus-go/config.yaml"
	PythonPath = "/app/aginctus-go"
)

//go:embed selective-go.yaml
var config string

//go:embed litellm_opencode_go_hook.py
var hook string

//go:embed opencode_go_forwarding.py
var forwarding string

// Files returns a new filename-to-raw-content map for public Compose configs.
// The canonical Python sources are embedded directly, without generated copies.
func Files() map[string]string {
	return map[string]string{
		"config.yaml":                 config,
		"litellm_opencode_go_hook.py": hook,
		"opencode_go_forwarding.py":   forwarding,
	}
}

// Manifest returns a new map of lowercase SHA-256 hex digests of Files' bytes.
// These hashes identify bundle contents, not the separately pinned OCI runtime.
func Manifest() map[string]string {
	result := make(map[string]string)
	for name, content := range Files() {
		result[name] = fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	}
	return result
}
