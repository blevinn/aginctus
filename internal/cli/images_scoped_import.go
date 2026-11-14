package cli

import (
	"context"
	"io"
	"os/exec"

	imageops "github.com/blevinn/aginctus/internal/cli/operations/images"
	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// Incus execution remains an injected CLI capability; it is always local,
// project-qualified, and deliberately omits image alias replacement flags.
var importScopedImage = func(ctx context.Context, scope incusscope.ProjectScope, metadata, rootfs, alias string) error {
	return exec.CommandContext(ctx, "incus", scopedImageImportArgs(scope, metadata, rootfs, alias)...).Run()
}

func scopedImageImportArgs(scope incusscope.ProjectScope, metadata, rootfs, alias string) []string {
	return imageops.ScopedImageImportArgs(scope, metadata, rootfs, alias)
}

func parseScopedImageImportArgs(args []string) (string, string, string, error) {
	return imageops.ParseScopedImageImportArgs(args)
}

func readScopedImageAlias(ctx context.Context, scope incusscope.ProjectScope, alias string) (string, bool, error) {
	return imageops.ReadScopedImageAlias(ctx, scope, alias, inspectScopedImages, maxProjectListBytes)
}

func verifyScopedImageProject(ctx context.Context, scope incusscope.ProjectScope) error {
	return imageops.VerifyScopedImageProject(ctx, scope, readLocalProjectAssessment)
}

func runScopedImageImport(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	return imageops.RunScopedImageImport(ctx, args, stdout, stderr, cfg, imageops.ScopedImportActions{
		Lease: acquireScopedImageMutationLease,
		Verify: verifyScopedImageProject,
		ReadAlias: readScopedImageAlias,
		Artifact: imageArtifact,
		Fingerprint: imageFingerprint,
		Import: importScopedImage,
	})
}
