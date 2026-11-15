package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/blevinn/aginctus/internal/config"
	imageops "github.com/blevinn/aginctus/internal/cli/operations/images"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// Ambient and project-scoped image read/preview adapters; ownership and project policy remain in operations.

// images.go — established CLI adapter surface.
type imageRunner func(context.Context, ...string) ([]byte, error)

var runIncusImage imageRunner = func(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "incus", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("incus %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

// Local CLI tests and callers retain these names while the artifact,
// identity, and presentation rules live in a dedicated operation package.
type localImage = imageops.LocalImage

func printLocalImages(stdout io.Writer, images []localImage) {
	imageops.PrintLocalImages(stdout, images)
}

func imageArtifact(dir, subdir, extension string) (string, error) {
	return imageops.Artifact(dir, subdir, extension)
}

func validImageName(name string) bool { return imageops.ValidImageName(name) }

func imageFingerprint(metadata, rootfs string) (string, error) {
	return imageops.Fingerprint(metadata, rootfs)
}

func runImages(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "list" || args[0] == "ls") {
		return imageops.ListLocalImages(ctx, stdout, stderr, runIncusImage)
	}
	if len(args) == 3 && args[0] == "update" {
		if err := updateLocalImage(ctx, args[1], args[2]); err != nil {
			fmt.Fprintf(stderr, "update image: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "image %q: updated\n", args[1])
		return 0
	}
	fmt.Fprintln(stderr, "Usage: aginctus images list|ls")
	fmt.Fprintln(stderr, "       aginctus images update <name> <path>")
	return 2
}

func localImages(ctx context.Context) ([]localImage, error) {
	return imageops.LocalImages(ctx, runIncusImage)
}

func temporaryImageAlias(name, role, nonce string) string {
	return imageops.TemporaryImageAlias(name, role, nonce)
}

func updateLocalImage(ctx context.Context, name, dir string) error {
	return imageops.UpdateLocalImage(ctx, name, dir, imageops.LegacyUpdateActions{
		Run: runIncusImage,
		Artifact: imageArtifact,
		Fingerprint: imageFingerprint,
		LocalImages: localImages,
	})
}

// images_scoped.go — established CLI adapter surface.
// A scoped read always uses the local daemon and a named project. Never
// dispatch through the legacy ambient-project image runner.
var inspectScopedImages = func(ctx context.Context, scope incusscope.ProjectScope) ([]byte, error) {
	return exec.CommandContext(ctx, "incus", scopedImageListArgs(scope)...).Output()
}

func scopedImageListArgs(scope incusscope.ProjectScope) []string {
	return imageops.ScopedImageListArgs(scope)
}

func parseScopedImageListArgs(args []string) (string, error) {
	return imageops.ParseScopedImageListArgs(args)
}

func runConfiguredImages(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	if len(args) > 0 && args[0] == "replace-plan" {
		return runImageReplacePlan(ctx, args, stdout, stderr, cfg)
	}
	if len(args) > 0 && args[0] == "import-owned" {
		return runScopedOwnedImageImport(ctx, args, stdout, stderr, cfg)
	}
	if len(args) > 0 && args[0] == "owner-audit" {
		return runImageOwnerAudit(ctx, args, stdout, stderr, cfg)
	}
	if len(args) > 0 && args[0] == "import" {
		return runScopedImageImport(ctx, args, stdout, stderr, cfg)
	}
	if len(args) < 2 || (args[0] != "list" && args[0] != "ls") {
		return runImages(ctx, args, stdout, stderr)
	}
	return imageops.ListScopedImages(ctx, args, stdout, stderr, cfg, readLocalProjectAssessment, inspectScopedImages, maxProjectListBytes)
}

// Root shims preserve tests and callers while ownership-claim decisions live
// with image operations and stay distinct from write authorization.
var ownedImageFingerprintPattern = imageops.OwnedFingerprintPattern

type observedClaimedImage = imageops.ObservedClaimedImage
type imageOwnerAuditReport = imageops.OwnerAuditReport

func expectedImageOwnerClaim(scope incusscope.ProjectScope, alias, owner string) map[string]string {
	return imageops.ExpectedOwnerClaim(scope, alias, owner)
}

func classifyImageOwnerClaim(images []observedClaimedImage, scope incusscope.ProjectScope, alias, owner string) string {
	return imageops.ClassifyOwnerClaim(images, scope, alias, owner)
}

func parseImageOwnerAuditArgs(args []string) (string, string, string, error) {
	return imageops.ParseOwnerAuditArgs(args)
}

func runImageOwnerAudit(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	return imageops.RunOwnerAudit(ctx, args, stdout, stderr, cfg, verifyScopedImageProject, inspectScopedImages, maxProjectListBytes)
}

// images_replace_plan.go — established CLI adapter surface.
// Keep the unit/controller observation bridge here until the privileged
// operator evidence implementation is separately extracted.
// No known conflict is NOT evidence of exclusive authority: every call is
// bounded to the system and current user's known four lifecycle units.
func surveyImageControllers(ctx context.Context, owner string) string {
	ownerSeen := false
	conflict, unknown := false, false
	for _, unit := range operatorLifecycleUnits {
		system := inspectOperatorUnitEvidence(ctx, "system", unit)
		user := inspectOperatorUnitEvidence(ctx, "user", unit)
		if system == unitUnknown || user == unitUnknown {
			unknown = true
		}
		selected, other := system, user
		if owner == "user" {
			selected, other = user, system
		}
		if other == unitActive || other == unitPresent {
			conflict = true
		}
		if selected == unitActive || selected == unitPresent {
			ownerSeen = true
		}
	}
	switch {
	case conflict:
		return "competing-known-unit"
	case unknown:
		return "unverified-unit-manager"
	case !ownerSeen:
		return "selected-owner-unit-not-observed"
	default:
		return "no-known-conflict"
	}
}

type imageReplacePlanReport = imageops.ReplacePlanReport

func parseImageReplacePlanArgs(args []string) (string, string, string, string, error) {
	return imageops.ParseReplacePlanArgs(args)
}

func imageAliasFingerprint(images []observedClaimedImage, name string) string {
	return imageops.AliasFingerprint(images, name)
}

func planOwnedImageReplacement(ctx context.Context, scope incusscope.ProjectScope, owner, alias, desired string, images []observedClaimedImage) imageReplacePlanReport {
	return imageops.PlanOwnedReplacement(ctx, scope, owner, alias, desired, images, surveyImageControllers)
}

func runImageReplacePlan(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	return imageops.RunReplacePlan(ctx, args, stdout, stderr, cfg, imageops.ReplacePlanActions{
		Verify:      verifyScopedImageProject,
		Inventory:   inspectOwnedScopedImageInventory,
		Artifact:    imageArtifact,
		Fingerprint: imageFingerprint,
		Survey:      surveyImageControllers,
	})
}
