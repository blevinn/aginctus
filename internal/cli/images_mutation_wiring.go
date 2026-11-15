package cli

import (
	"context"
	"io"
	"os/exec"

	"github.com/blevinn/aginctus/internal/config"
	imageops "github.com/blevinn/aginctus/internal/cli/operations/images"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// Explicitly scoped create-only image mutation and project-wide writer-lease adapters; preserve injected command seams.

// images_scoped_import.go — established CLI adapter surface.
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

// images_scoped_lease.go — established CLI adapter surface.
// Root compatibility seams allow existing CLI regression tests to substitute
// the cooperative project lease without changing the actual image operation.
func scopedImageLeaseAddress(scope incusscope.ProjectScope) (string, error) {
	return imageops.ScopedImageLeaseAddress(scope)
}
var acquireScopedImageMutationLease = imageops.AcquireScopedImageMutationLease

// images_scoped_owned_import.go — established CLI adapter surface.
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
