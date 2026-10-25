package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"time"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// importOwnedScopedImage performs exactly one project-local create request.
// The positional key=value arguments are Incus image properties supplied at
// creation, not a later property rewrite of a potentially shared image.
var importOwnedScopedImage = func(ctx context.Context, scope incusscope.ProjectScope, metadata, rootfs, alias, owner string) error {
	return exec.CommandContext(ctx, "incus", scopedOwnedImageImportArgs(scope, metadata, rootfs, alias, owner)...).Run()
}

func scopedOwnedImageImportArgs(scope incusscope.ProjectScope, metadata, rootfs, alias, owner string) []string {
	props := expectedImageOwnerClaim(scope, alias, owner)
	keys := make([]string, 0, len(props))
	for key := range props {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := []string{"--force-local", "--project", scope.Name, "image", "import", metadata, rootfs}
	for _, key := range keys {
		args = append(args, key+"="+props[key])
	}
	// Incus --reuse would replace an existing alias; never pass it.
	return append(args, "--alias", alias)
}

func parseScopedOwnedImageImportArgs(args []string) (project, owner, alias, dir string, err error) {
	if len(args) != 8 || args[0] != "import-owned" || args[1] != "--project" ||
		args[3] != "--owner" || (args[4] != "system" && args[4] != "user") ||
		args[7] != "--confirm-create" || !validImageName(args[5]) || args[6] == "" {
		return "", "", "", "", fmt.Errorf("explicit project, system/user claim, alias, artifact path and confirmation required")
	}
	if _, err := incusscope.NewProjectScope(args[2]); err != nil {
		return "", "", "", "", err
	}
	return args[2], args[4], args[5], args[6], nil
}

func inspectOwnedScopedImageInventory(ctx context.Context, scope incusscope.ProjectScope) ([]observedClaimedImage, error) {
	raw, err := inspectScopedImages(ctx, scope)
	if err != nil || len(raw) > maxProjectListBytes {
		return nil, fmt.Errorf("unable to inspect project-local images")
	}
	var images []observedClaimedImage
	if err := json.Unmarshal(raw, &images); err != nil || images == nil {
		return nil, fmt.Errorf("invalid project-local image inventory")
	}
	// An incomplete record invalidates the entire snapshot; it must not
	// silently mean an absent target fingerprint or alias.
	for _, image := range images {
		if !ownedImageFingerprintPattern.MatchString(image.Fingerprint) {
			return nil, fmt.Errorf("project-local image identity unverified")
		}
	}
	return images, nil
}

func availableForOwnedImport(images []observedClaimedImage, alias, fingerprint string) bool {
	for _, image := range images {
		// A matching image in the target project must never be adopted or
		// have its properties modified as part of a new import.
		if image.Fingerprint == fingerprint {
			return false
		}
		for _, item := range image.Aliases {
			if item.Name == alias {
				return false
			}
		}
	}
	return true
}

// This command creates a fresh image record with a *self-declared* owner
// claim. Neither the owner flag nor the properties authenticate the caller
// or prove an active systemd/Home Manager controller. As with the older
// create-only import, do not automatically delete uncertain resources.
func runScopedOwnedImageImport(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	project, owner, alias, dir, err := parseScopedOwnedImageImportArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, "Usage: aginctus images import-owned --project NAME --owner system|user ALIAS PATH --confirm-create")
		return 2
	}
	scope, err := cfg.IncusProjectScope()
	if err != nil || scope.Name != project {
		fmt.Fprintln(stderr, "images import-owned: project must match the declared incus.project setting")
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err := verifyScopedImageProject(ctx, scope); err != nil {
		fmt.Fprintln(stderr, "images import-owned: local project not verified; no import attempted")
		return 1
	}
	initial, err := inspectOwnedScopedImageInventory(ctx, scope)
	if err != nil {
		fmt.Fprintln(stderr, "images import-owned: cannot verify project-local image inventory")
		return 1
	}
	metadata, err := imageArtifact(dir, "metadata", ".tar.xz")
	if err != nil {
		fmt.Fprintln(stderr, "images import-owned: cannot resolve metadata artifact")
		return 1
	}
	rootfs, err := imageArtifact(dir, "rootfs", ".squashfs")
	if err != nil {
		fmt.Fprintln(stderr, "images import-owned: cannot resolve rootfs artifact")
		return 1
	}
	fingerprint, err := imageFingerprint(metadata, rootfs)
	if err != nil {
		fmt.Fprintln(stderr, "images import-owned: cannot fingerprint image artifacts")
		return 1
	}
	if !availableForOwnedImport(initial, alias, fingerprint) {
		fmt.Fprintln(stderr, "images import-owned: alias or fingerprint already exists; refusing adoption")
		return 1
	}

	if err := verifyScopedImageProject(ctx, scope); err != nil {
		fmt.Fprintln(stderr, "images import-owned: project changed before import; no mutation attempted")
		return 1
	}
	beforeWrite, err := inspectOwnedScopedImageInventory(ctx, scope)
	if err != nil || !availableForOwnedImport(beforeWrite, alias, fingerprint) {
		fmt.Fprintln(stderr, "images import-owned: image inventory changed or unavailable; refusing mutation")
		return 1
	}
	// Incus's server-side duplicate fingerprint/alias checks are the
	// remaining collision defense against another writer.
	if err := importOwnedScopedImage(ctx, scope, metadata, rootfs, alias, owner); err != nil {
		fmt.Fprintln(stderr, "images import-owned: creation refused or failed; inspect state manually before retrying")
		return 1
	}

	if err := verifyScopedImageProject(ctx, scope); err != nil {
		fmt.Fprintln(stderr, "images import-owned: project unverified after creation; no automatic rollback")
		return 1
	}
	after, err := inspectOwnedScopedImageInventory(ctx, scope)
	if err != nil || classifyImageOwnerClaim(after, scope, alias, owner) != "claimed" {
		fmt.Fprintln(stderr, "images import-owned: ownership claim not verified after creation; no automatic rollback")
		return 1
	}
	found := false
	for _, image := range after {
		if image.Fingerprint == fingerprint {
			for _, item := range image.Aliases {
				if item.Name == alias {
					found = true
				}
			}
		}
	}
	if !found {
		fmt.Fprintln(stderr, "images import-owned: fingerprint mismatch after creation; no automatic rollback")
		return 1
	}
	fmt.Fprintf(stdout, "image %q: created with %q claim in project %q\n", alias, owner, scope.Name)
	return 0
}
