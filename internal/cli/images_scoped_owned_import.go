package cli

import (
	"context"
	"io"
	"os/exec"

	imageops "github.com/blevinn/aginctus/internal/cli/operations/images"
	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// The existing test-injectable Incus executor remains explicit. No --reuse
// or implicit remote can enter the authoritative owned-import operation.
var importOwnedScopedImage = func(ctx context.Context, scope incusscope.ProjectScope, metadata, rootfs, alias, owner string) error {
	return exec.CommandContext(ctx, "incus", scopedOwnedImageImportArgs(scope, metadata, rootfs, alias, owner)...).Run()
}

func scopedOwnedImageImportArgs(scope incusscope.ProjectScope, metadata, rootfs, alias, owner string) []string {
	return imageops.ScopedOwnedImageImportArgs(scope, metadata, rootfs, alias, owner)
}

func parseScopedOwnedImageImportArgs(args []string) (string, string, string, string, error) {
	return imageops.ParseScopedOwnedImageImportArgs(args)
}

func inspectOwnedScopedImageInventory(ctx context.Context, scope incusscope.ProjectScope) ([]observedClaimedImage, error) {
	return imageops.InspectOwnedScopedImageInventory(ctx, scope, inspectScopedImages, maxProjectListBytes)
}

func availableForOwnedImport(images []observedClaimedImage, alias, fingerprint string) bool {
	return imageops.AvailableForOwnedImport(images, alias, fingerprint)
}

func runScopedOwnedImageImport(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	return imageops.RunScopedOwnedImageImport(ctx, args, stdout, stderr, cfg, imageops.OwnedImportActions{
		Lease: acquireScopedImageMutationLease,
		Verify: verifyScopedImageProject,
		Inventory: inspectOwnedScopedImageInventory,
		Artifact: imageArtifact,
		Fingerprint: imageFingerprint,
		Import: importOwnedScopedImage,
	})
}
