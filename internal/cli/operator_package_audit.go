package cli

import (
	"io"
	"os"

	operatorops "github.com/blevinn/aginctus/internal/cli/operations/operator"
)

const operatorSystemPackageManifest = operatorops.SystemPackageManifest
const maxOperatorPackageManifestSize = operatorops.MaxPackageManifestSize
var operatorPackageStorePath = operatorops.PackageStorePath
var inspectOperatorExecutable = os.Executable

type operatorPackageManifest = operatorops.PackageManifest
type operatorPackageEvidence = operatorops.PackageEvidence

const (
	operatorPackageAbsent operatorPackageEvidence = operatorops.PackageAbsent
	operatorPackageDeclared operatorPackageEvidence = operatorops.PackageDeclared
	operatorPackageUnverified operatorPackageEvidence = operatorops.PackageUnverified
)

func observedOperatorPackage() (string, bool) {
	return operatorops.ObservedPackage(inspectOperatorExecutable)
}

func inspectOperatorPackageManifest(path, expectedAuthority string) (operatorPackageEvidence, string) {
	return operatorops.InspectPackageManifest(path, expectedAuthority)
}

func parseOperatorPackageAuditArgs(args []string) (string, bool) {
	return operatorops.ParsePackageAuditArgs(args)
}

func runOperatorPackageAudit(args []string, stdout, stderr io.Writer) int {
	return operatorops.RunPackageAudit(args, stdout, stderr, os.Geteuid, os.UserConfigDir, inspectOperatorExecutable)
}

func renderOperatorPackageAudit(owner, systemPath, userPath string, stdout, stderr io.Writer) int {
	return operatorops.RenderPackageAudit(owner, systemPath, userPath, stdout, stderr, inspectOperatorExecutable)
}
