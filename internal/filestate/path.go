package filestate

import (
	"fmt"
	"os"
	"path/filepath"
)

// StateRoot resolves the directory that holds Aginctus state for the current
// user. XDG_STATE_HOME wins; otherwise the root is the XDG default state
// directory under the user's home.
//
// A relative result is rejected rather than cleaned up. Silently joining a
// relative root would place secret material under the caller's working
// directory, which is never an operator intent, and both the provider registry
// and the gateway secret state depend on that failure.
func StateRoot() (string, error) {
	root := os.Getenv("XDG_STATE_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory for aginctus state: %w", err)
		}
		root = filepath.Join(home, ".local", "state")
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("state directory must be absolute")
	}
	return root, nil
}
