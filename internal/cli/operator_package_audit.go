package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

const operatorSystemPackageManifest = "/etc/aginctus/operator-package.json"
const maxOperatorPackageManifestSize = 4096

// The package identity is a declared Nix store output, never a credential.
// It is not evidence that the corresponding binary is currently running.
var operatorPackageStorePath = regexp.MustCompile(`^/nix/store/[0-9a-z]{32}-[A-Za-z0-9][A-Za-z0-9+._-]*$`)

// Observe this process through the OS, never argv[0], PATH or a declaration.
var inspectOperatorExecutable = os.Executable

func observedOperatorPackage() (string, bool) {
	executable, err := inspectOperatorExecutable()
	if err != nil || filepath.Clean(executable) != executable {
		return "", false
	}
	// The packaged CLI uses makeWrapper; the kernel observes its wrapped binary.
	if filepath.Base(executable) != "aginctus" && filepath.Base(executable) != ".aginctus-wrapped" {
		return "", false
	}
	bin := filepath.Dir(executable)
	pkg := filepath.Dir(bin)
	if filepath.Base(bin) != "bin" || !operatorPackageStorePath.MatchString(pkg) {
		return "", false
	}
	return pkg, true
}

type operatorPackageManifest struct {
	Schema    int    `json:"schema"`
	Authority string `json:"authority"`
	Package   string `json:"package"`
}

type operatorPackageEvidence string

const (
	operatorPackageAbsent     operatorPackageEvidence = "absent"
	operatorPackageDeclared   operatorPackageEvidence = "declared"
	operatorPackageUnverified operatorPackageEvidence = "unverified"
)

// inspectOperatorPackageManifest only reads this public, dedicated JSON
// declaration; it does not inspect a service's argv, Environment or secrets.
// Invalid/unreadable content is never treated as a missing declaration.
func inspectOperatorPackageManifest(path, expectedAuthority string) (operatorPackageEvidence, string) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return operatorPackageAbsent, ""
	}
	if err != nil {
		return operatorPackageUnverified, ""
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return operatorPackageUnverified, ""
	}
	data, err := io.ReadAll(io.LimitReader(file, maxOperatorPackageManifestSize+1))
	if err != nil || len(data) > maxOperatorPackageManifestSize {
		return operatorPackageUnverified, ""
	}
	var declared operatorPackageManifest
	if err := json.Unmarshal(data, &declared); err != nil ||
		declared.Schema != 1 || declared.Authority != expectedAuthority ||
		!operatorPackageStorePath.MatchString(declared.Package) {
		return operatorPackageUnverified, ""
	}
	return operatorPackageDeclared, declared.Package
}

func parseOperatorPackageAuditArgs(args []string) (string, bool) {
	switch {
	case len(args) == 1 && args[0] == "--owner=system":
		return "system", true
	case len(args) == 1 && args[0] == "--owner=user":
		return "user", true
	case len(args) == 2 && args[0] == "--owner" &&
		(args[1] == "system" || args[1] == "user"):
		return args[1], true
	default:
		return "", false
	}
}

// A declaration can be compared from the operator's ordinary user account.
// Root's XDG configuration is not the intended Home Manager user profile.
func runOperatorPackageAudit(args []string, stdout, stderr io.Writer) int {
	owner, ok := parseOperatorPackageAuditArgs(args)
	if !ok {
		fmt.Fprintln(stderr, "Usage: aginctus operator package-audit --owner system|user")
		return 2
	}
	if os.Geteuid() == 0 {
		fmt.Fprintln(stderr, "operator package-audit: run as the Home Manager operator, not root")
		return 2
	}
	userConfigRoot, err := os.UserConfigDir()
	if err != nil || !filepath.IsAbs(userConfigRoot) {
		fmt.Fprintln(stderr, "operator package-audit: user configuration directory unavailable")
		return 1
	}
	return renderOperatorPackageAudit(owner, operatorSystemPackageManifest,
		filepath.Join(userConfigRoot, "aginctus", "operator-package.json"), stdout, stderr)
}

func renderOperatorPackageAudit(owner, systemPath, userPath string, stdout, stderr io.Writer) int {
	if owner != "system" && owner != "user" {
		fmt.Fprintln(stderr, "operator package-audit: invalid authority")
		return 2
	}
	systemState, systemPackage := inspectOperatorPackageManifest(systemPath, "system")
	userState, userPackage := inspectOperatorPackageManifest(userPath, "user")
	fmt.Fprintf(stdout, "Declared operator package identity: system=%s user=%s\n", systemState, userState)

	selected := systemState
	selectedPackage := systemPackage
	if owner == "user" {
		selected = userState
		selectedPackage = userPackage
	}
	if selected != operatorPackageDeclared ||
		systemState == operatorPackageUnverified || userState == operatorPackageUnverified {
		fmt.Fprintln(stderr, "operator package authority missing or unverified; cannot establish a compatible declaration")
		return 1
	}
	if systemState == operatorPackageDeclared && userState == operatorPackageDeclared &&
		systemPackage != userPackage {
		fmt.Fprintln(stderr, "operator package declarations differ: system/user CLI version compatibility is unverified")
		return 1
	}
	observedPackage, ok := observedOperatorPackage()
	if !ok {
		fmt.Fprintln(stderr, "running CLI package identity is unverified; invoke the selected Nix profile's Aginctus CLI and repeat this read-only audit")
		return 1
	}
	if observedPackage != selectedPackage {
		fmt.Fprintln(stderr, "running CLI differs from the selected package declaration; align the explicitly selected CLI/profile before further preflight")
		return 1
	}
	fmt.Fprintln(stdout, "Declared package outputs are consistent with the observed running CLI path; service executables, protocol compatibility, ownership and write authority remain unverified")
	return 0
}
