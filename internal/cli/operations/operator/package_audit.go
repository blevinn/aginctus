package operatorops

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

const SystemPackageManifest = "/etc/aginctus/operator-package.json"
const MaxPackageManifestSize = 4096

// The package identity is a declared Nix store output, never a credential.
// It is not evidence that the corresponding binary is currently running.
var PackageStorePath = regexp.MustCompile(`^/nix/store/[0-9a-z]{32}-[A-Za-z0-9][A-Za-z0-9+._-]*$`)

// Observe this process through the OS, never argv[0], PATH or a declaration.
func ObservedPackage(inspect func() (string, error)) (string, bool) {
	executable, err := inspect()
	if err != nil || filepath.Clean(executable) != executable {
		return "", false
	}
	// The packaged CLI uses makeWrapper; the kernel observes its wrapped binary.
	if filepath.Base(executable) != "aginctus" && filepath.Base(executable) != ".aginctus-wrapped" {
		return "", false
	}
	bin := filepath.Dir(executable)
	pkg := filepath.Dir(bin)
	if filepath.Base(bin) != "bin" || !PackageStorePath.MatchString(pkg) {
		return "", false
	}
	return pkg, true
}

type PackageManifest struct {
	Schema    int    `json:"schema"`
	Authority string `json:"authority"`
	Package   string `json:"package"`
}

type PackageEvidence string

const (
	PackageAbsent     PackageEvidence = "absent"
	PackageDeclared   PackageEvidence = "declared"
	PackageUnverified PackageEvidence = "unverified"
)

// InspectPackageManifest only reads this public, dedicated JSON
// declaration; it does not inspect a service's argv, Environment or secrets.
// Invalid/unreadable content is never treated as a missing declaration.
func InspectPackageManifest(path, expectedAuthority string) (PackageEvidence, string) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return PackageAbsent, ""
	}
	if err != nil {
		return PackageUnverified, ""
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return PackageUnverified, ""
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxPackageManifestSize+1))
	if err != nil || len(data) > MaxPackageManifestSize {
		return PackageUnverified, ""
	}
	var declared PackageManifest
	if err := json.Unmarshal(data, &declared); err != nil ||
		declared.Schema != 1 || declared.Authority != expectedAuthority ||
		!PackageStorePath.MatchString(declared.Package) {
		return PackageUnverified, ""
	}
	return PackageDeclared, declared.Package
}

func ParsePackageAuditArgs(args []string) (string, bool) {
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
func RunPackageAudit(args []string, stdout, stderr io.Writer, effectiveUID func() int, userConfigDir func() (string, error), executable func() (string, error)) int {
	owner, ok := ParsePackageAuditArgs(args)
	if !ok {
		fmt.Fprintln(stderr, "Usage: aginctus operator package-audit --owner system|user")
		return 2
	}
	if effectiveUID() == 0 {
		fmt.Fprintln(stderr, "operator package-audit: run as the Home Manager operator, not root")
		return 2
	}
	userConfigRoot, err := userConfigDir()
	if err != nil || !filepath.IsAbs(userConfigRoot) {
		fmt.Fprintln(stderr, "operator package-audit: user configuration directory unavailable")
		return 1
	}
	return RenderPackageAudit(owner, SystemPackageManifest,
		filepath.Join(userConfigRoot, "aginctus", "operator-package.json"), stdout, stderr, executable)
}

func RenderPackageAudit(owner, systemPath, userPath string, stdout, stderr io.Writer, executable func() (string, error)) int {
	if owner != "system" && owner != "user" {
		fmt.Fprintln(stderr, "operator package-audit: invalid authority")
		return 2
	}
	systemState, systemPackage := InspectPackageManifest(systemPath, "system")
	userState, userPackage := InspectPackageManifest(userPath, "user")
	fmt.Fprintf(stdout, "Declared operator package identity: system=%s user=%s\n", systemState, userState)

	selected := systemState
	selectedPackage := systemPackage
	if owner == "user" {
		selected = userState
		selectedPackage = userPackage
	}
	if selected != PackageDeclared ||
		systemState == PackageUnverified || userState == PackageUnverified {
		fmt.Fprintln(stderr, "operator package authority missing or unverified; cannot establish a compatible declaration")
		return 1
	}
	if systemState == PackageDeclared && userState == PackageDeclared &&
		systemPackage != userPackage {
		fmt.Fprintln(stderr, "operator package declarations differ: system/user CLI version compatibility is unverified")
		return 1
	}
	observedPackage, ok := ObservedPackage(executable)
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
