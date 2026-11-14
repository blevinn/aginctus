package cli

import (
	"context"
	"io"
	"os/exec"

	imageops "github.com/blevinn/aginctus/internal/cli/operations/images"
	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

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
