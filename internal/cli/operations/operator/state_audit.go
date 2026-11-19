package operatorops

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"

	"github.com/blevinn/aginctus/internal/config"
)

const SystemStateRoot = "/var/lib/aginctus/state"

var GatewayIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// GatewayStateEvidence is metadata-only: never open, deserialize, or log a
// gateway secrets file. "present" establishes no active reconciler ownership.
type GatewayStateEvidence string

const (
	GatewayStateAbsent     GatewayStateEvidence = "absent"
	GatewayStatePresent    GatewayStateEvidence = "present"
	GatewayStateIncomplete GatewayStateEvidence = "incomplete"
	GatewayStateUnverified GatewayStateEvidence = "unverified"
	GatewayStateUnsafe     GatewayStateEvidence = "unsafe"
)

func InspectGatewayState(root, gatewayID string, expectedUID uint32) GatewayStateEvidence {
	if !filepath.IsAbs(root) || root == "/" || !GatewayIDPattern.MatchString(gatewayID) {
		return GatewayStateUnsafe
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
			return GatewayStateAbsent
		}
		if err != nil {
			return GatewayStateUnverified
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return GatewayStateUnsafe
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != expectedUID || info.Mode().Perm()&0o022 != 0 {
			return GatewayStateUnsafe
		}
		if index == len(dirs)-1 && info.Mode().Perm()&0o077 != 0 {
			return GatewayStateUnsafe
		}
	}

	// lstat only: the secret file is not opened or read, even when the
	// operator has permission. Symlinked/nonregular and overbroad files
	// are rejected instead of being followed or repaired.
	info, err := os.Lstat(filepath.Join(dirs[len(dirs)-1], "secrets.json"))
	if errors.Is(err, os.ErrNotExist) {
		return GatewayStateIncomplete
	}
	if err != nil {
		return GatewayStateUnverified
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Uid != expectedUID ||
		info.Mode().Perm()&0o177 != 0 {
		return GatewayStateUnsafe
	}
	return GatewayStatePresent
}

// RunStateAudit inspects exactly one explicitly selected credential-storage
// location. This is metadata observation, not Incus project or lifecycle
// authority. Host inspection requires root; user inspection requires the
// unprivileged user whose state root is being examined.
func RunStateAudit(args []string, stdout, stderr io.Writer, effective *config.Config, euid func() int, stateRoot func() (string, error)) int {
	location := ""
	switch {
	case len(args) == 2 && args[0] == "state-audit" && args[1] == "--location=system":
		location = "system"
	case len(args) == 2 && args[0] == "state-audit" && args[1] == "--location=user":
		location = "user"
	case len(args) == 3 && args[0] == "state-audit" && args[1] == "--location" &&
		(args[2] == "system" || args[2] == "user"):
		location = args[2]
	default:
		fmt.Fprintln(stderr, "Usage: aginctus operator state-audit --location system|user")
		return 2
	}
	uid := euid()
	if (location == "system" && uid != 0) || (location == "user" && uid == 0) {
		fmt.Fprintln(stderr, "operator state-audit: inspect host credentials as root or user credentials as the unprivileged owner; no privilege fallback")
		return 2
	}
	gatewayID, err := effective.String("gateway.id")
	if err != nil || !GatewayIDPattern.MatchString(gatewayID) {
		fmt.Fprintln(stderr, "operator state-audit: gateway.id must be an explicit, valid gateway identifier")
		return 2
	}
	root := SystemStateRoot
	if location == "user" {
		root, err = stateRoot()
		if err != nil || !filepath.IsAbs(root) {
			fmt.Fprintln(stderr, "operator state-audit: cannot resolve absolute user state root")
			return 2
		}
	}
	return RenderStateAudit(location, root, gatewayID, uint32(uid), stdout, stderr)
}

// RenderStateAudit inspects metadata only and never reads secret bytes.
// A credential tree does not prove active service use or reconciler ownership.
func RenderStateAudit(location, root, gatewayID string, uid uint32, stdout, stderr io.Writer) int {
	if location != "system" && location != "user" {
		fmt.Fprintln(stderr, "operator state-audit: invalid credential location")
		return 2
	}
	result := InspectGatewayState(root, gatewayID, uid)
	fmt.Fprintf(stdout, "Gateway %q: credential storage location %s (read-only)\n", gatewayID, location)
	fmt.Fprintf(stdout, "%s gateway credential metadata: %s\n", map[string]string{"system":"System", "user":"User"}[location], result)
	if result == GatewayStateUnsafe || result == GatewayStateUnverified || result == GatewayStateIncomplete {
		fmt.Fprintln(stderr, "gateway credential metadata unsafe or unverified; no secret contents read")
		return 1
	}
	fmt.Fprintln(stdout, "Credential metadata observed; neither running service nor Incus project authority verified")
	return 0
}
