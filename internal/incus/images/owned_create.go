package images

import (
	"context"
	"fmt"
	"time"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// OwnedCreateActions contains exact-project creation capabilities. Self-declared
// owner claims and a cooperative writer lease never constitute authorization.
type OwnedCreateActions struct {
	Lease func(incusscope.ProjectScope) (func(), error)
	Verify func(context.Context, incusscope.ProjectScope) error
	Inventory func(context.Context, incusscope.ProjectScope) ([]ObservedClaimedImage, error)
	Artifact func(string, string, string) (string, error)
	Fingerprint func(string, string) (string, error)
	Import func(context.Context, incusscope.ProjectScope, string, string, string, string) error
}

// CreateClaimedImage never adopts an image or overwrites an alias. It holds
// the cooperative lease through an independently verified post-create claim
// and fingerprint; on uncertain state it deliberately does not roll back.
func CreateClaimedImage(ctx context.Context, scope incusscope.ProjectScope, owner, alias, dir string, actions OwnedCreateActions) error {
	if actions.Lease == nil || actions.Verify == nil || actions.Inventory == nil ||
		actions.Artifact == nil || actions.Fingerprint == nil || actions.Import == nil {
		return fmt.Errorf("images import-owned: required capabilities unavailable; no import attempted")
	}
	// Serialize all participating image writers in this local project,
	// including distinct aliases which could have matching fingerprints.
	release, err := actions.Lease(scope)
	if err != nil || release == nil {
		return fmt.Errorf("images import-owned: project writer lease unavailable; no import attempted")
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err := actions.Verify(ctx, scope); err != nil {
		return fmt.Errorf("images import-owned: local project not verified; no import attempted")
	}
	initial, err := actions.Inventory(ctx, scope)
	if err != nil {
		return fmt.Errorf("images import-owned: cannot verify project-local image inventory")
	}
	metadata, err := actions.Artifact(dir, "metadata", ".tar.xz")
	if err != nil {
		return fmt.Errorf("images import-owned: cannot resolve metadata artifact")
	}
	rootfs, err := actions.Artifact(dir, "rootfs", ".squashfs")
	if err != nil {
		return fmt.Errorf("images import-owned: cannot resolve rootfs artifact")
	}
	fingerprint, err := actions.Fingerprint(metadata, rootfs)
	if err != nil {
		return fmt.Errorf("images import-owned: cannot fingerprint image artifacts")
	}
	if !AvailableForOwnedImport(initial, alias, fingerprint) {
		return fmt.Errorf("images import-owned: alias or fingerprint already exists; refusing adoption")
	}

	if err := actions.Verify(ctx, scope); err != nil {
		return fmt.Errorf("images import-owned: project changed before import; no mutation attempted")
	}
	beforeWrite, err := actions.Inventory(ctx, scope)
	if err != nil || !AvailableForOwnedImport(beforeWrite, alias, fingerprint) {
		return fmt.Errorf("images import-owned: image inventory changed or unavailable; refusing mutation")
	}
	// Incus's server-side duplicate fingerprint/alias checks are the
	// remaining collision defense against another writer.
	if err := actions.Import(ctx, scope, metadata, rootfs, alias, owner); err != nil {
		return fmt.Errorf("images import-owned: creation refused or failed; inspect state manually before retrying")
	}

	if err := actions.Verify(ctx, scope); err != nil {
		return fmt.Errorf("images import-owned: project unverified after creation; no automatic rollback")
	}
	after, err := actions.Inventory(ctx, scope)
	if err != nil || ClassifyOwnerClaim(after, scope, alias, owner) != "claimed" {
		return fmt.Errorf("images import-owned: ownership claim not verified after creation; no automatic rollback")
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
		return fmt.Errorf("images import-owned: fingerprint mismatch after creation; no automatic rollback")
	}
	return nil
}
