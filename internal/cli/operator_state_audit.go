package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/filestate"
)

const operatorSystemStateRoot = "/var/lib/aginctus/state"

var operatorGatewayID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// gatewayStateEvidence is metadata-only: never open, deserialize, or log a
// gateway secrets file. "present" establishes no active reconciler ownership.
type gatewayStateEvidence string

const (
	gatewayStateAbsent      gatewayStateEvidence = "absent"
	gatewayStatePresent     gatewayStateEvidence = "present"
	gatewayStateIncomplete  gatewayStateEvidence = "incomplete"
	gatewayStateUnverified  gatewayStateEvidence = "unverified"
	gatewayStateUnsafe      gatewayStateEvidence = "unsafe"
)

func inspectGatewayState(root, gatewayID string, expectedUID uint32) gatewayStateEvidence {
	if !filepath.IsAbs(root) || root == "/" || !operatorGatewayID.MatchString(gatewayID) {
		return gatewayStateUnsafe
	}

	// Lstat each component in the state tree before examining the next:
	// never follow a symlink in the root or Aginctus subtree.
	dirs := []string{
		filepath.Clean(root),
		filepath.Join(root, "aginctus"),
		filepath.Join(root, "aginctus", "gateway"),
		filepath.Join(root, "aginctus", "gateway", gatewayID),
	}
	for index, path := range dirs {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return gatewayStateAbsent
		}
		if err != nil {
			return gatewayStateUnverified
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return gatewayStateUnsafe
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != expectedUID || info.Mode().Perm()&0o022 != 0 {
			return gatewayStateUnsafe
		}
		if index == len(dirs)-1 && info.Mode().Perm()&0o077 != 0 {
			return gatewayStateUnsafe
		}
	}

	// lstat only: the secret file is not opened or read, even when the
	// operator has permission. Symlinked/nonregular and overbroad files
	// are rejected instead of being followed or repaired.
	info, err := os.Lstat(filepath.Join(dirs[len(dirs)-1], "secrets.json"))
	if errors.Is(err, os.ErrNotExist) {
		return gatewayStateIncomplete
	}
	if err != nil {
		return gatewayStateUnverified
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Uid != expectedUID ||
		info.Mode().Perm()&0o177 != 0 {
		return gatewayStateUnsafe
	}
	return gatewayStatePresent
}

func auditOperatorGatewayState(owner, systemRoot, userRoot, gatewayID string, systemUID, userUID uint32) (gatewayStateEvidence, gatewayStateEvidence, bool) {
	// The state root selected for the user may not be the same location as
	// system-owned state; otherwise a user environment variable could make
	// one credential tree appear to have two owners.
	if filepath.Clean(systemRoot) == filepath.Clean(userRoot) {
		return gatewayStateUnsafe, gatewayStateUnsafe, false
	}
	system := inspectGatewayState(systemRoot, gatewayID, systemUID)
	user := inspectGatewayState(userRoot, gatewayID, userUID)
	if system == gatewayStateUnsafe || user == gatewayStateUnsafe ||
		system == gatewayStateUnverified || user == gatewayStateUnverified ||
		system == gatewayStateIncomplete || user == gatewayStateIncomplete {
		return system, user, false
	}
	switch owner {
	case "system":
		return system, user, user == gatewayStateAbsent
	case "user":
		return system, user, system == gatewayStateAbsent
	default:
		return system, user, false
	}
}

// runOperatorStateAudit is a standalone read-only diagnostic. Selecting an
// owner does not activate that owner's gateway, adopt an Incus object or grant
// access to protected state. It is not yet a guard on the mutating commands.
func runOperatorStateAudit(args []string, stdout, stderr io.Writer, effective *config.Config) int {
	owner := ""
	switch {
	case len(args) == 2 && args[0] == "state-audit" && args[1] == "--owner=system":
		owner = "system"
	case len(args) == 2 && args[0] == "state-audit" && args[1] == "--owner=user":
		owner = "user"
	case len(args) == 3 && args[0] == "state-audit" && args[1] == "--owner" &&
		(args[2] == "system" || args[2] == "user"):
		owner = args[2]
	default:
		fmt.Fprintln(stderr, "Usage: aginctus operator state-audit --owner system|user")
		return 2
	}
	if owner == "user" && os.Geteuid() == 0 {
		fmt.Fprintln(stderr, "operator state-audit: user ownership must be checked as the unprivileged operator (no sudo)")
		return 2
	}
	gatewayID, err := effective.String("gateway.id")
	if err != nil || !operatorGatewayID.MatchString(gatewayID) {
		fmt.Fprintln(stderr, "operator state-audit: gateway.id must be an explicit, valid gateway identifier")
		return 2
	}
	userRoot, err := filestate.StateRoot()
	if err != nil {
		fmt.Fprintln(stderr, "operator state-audit: cannot resolve absolute user state root")
		return 2
	}
	return renderOperatorStateAudit(owner, operatorSystemStateRoot, userRoot, gatewayID, 0, uint32(os.Geteuid()), stdout, stderr)
}

func renderOperatorStateAudit(owner, systemRoot, userRoot, gatewayID string, systemUID, userUID uint32, stdout, stderr io.Writer) int {
	system, user, ok := auditOperatorGatewayState(owner, systemRoot, userRoot, gatewayID, systemUID, userUID)
	fmt.Fprintf(stdout, "Gateway %q: selected state authority %s (read-only; no migration)\n", gatewayID, owner)
	fmt.Fprintf(stdout, "System gateway credential metadata: %s\n", system)
	fmt.Fprintf(stdout, "User gateway credential metadata: %s\n", user)
	if !ok {
		fmt.Fprintln(stderr, "gateway state ownership unverified or conflicting: stop before any user/system handoff; inspect through the authorized owner (no secret contents)")
		return 1
	}
	fmt.Fprintln(stdout, "No competing credential state observed; running unit, Incus alias, and reconciler authority remain unverified")
	return 0
}
