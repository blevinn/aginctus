package images

import (
	"context"
	"time"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// OwnedCreateFailure is a stable, typed domain refusal. The CLI owns human
// diagnostic formatting; individual capability errors are never exposed.
type OwnedCreateFailure string

func (e OwnedCreateFailure) Error() string { return string(e) }

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
	// Domain callers must not rely on a particular CLI parser to constrain
	// the selected project, self-declared owner or imported alias.
	if _, err := incusscope.NewProjectScope(scope.Name); err != nil ||
		(owner != "system" && owner != "user") || !ValidImageName(alias) || dir == "" {
		return OwnedCreateFailure("selection")
	}
	if actions.Lease == nil || actions.Verify == nil || actions.Inventory == nil ||
		actions.Artifact == nil || actions.Fingerprint == nil || actions.Import == nil {
		return OwnedCreateFailure("capabilities")
	}
	// Serialize all participating image writers in this local project,
	// including distinct aliases which could have matching fingerprints.
	release, err := actions.Lease(scope)
	if err != nil || release == nil {
		return OwnedCreateFailure("lease")
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err := actions.Verify(ctx, scope); err != nil {
		return OwnedCreateFailure("project-initial")
	}
	initial, err := actions.Inventory(ctx, scope)
	if err != nil {
		return OwnedCreateFailure("inventory-initial")
	}
	metadata, err := actions.Artifact(dir, "metadata", ".tar.xz")
	if err != nil {
		return OwnedCreateFailure("metadata")
	}
	rootfs, err := actions.Artifact(dir, "rootfs", ".squashfs")
	if err != nil {
		return OwnedCreateFailure("rootfs")
	}
	fingerprint, err := actions.Fingerprint(metadata, rootfs)
	if err != nil {
		return OwnedCreateFailure("fingerprint")
	}
	if !AvailableForOwnedImport(initial, alias, fingerprint) {
		return OwnedCreateFailure("adoption")
	}

	if err := actions.Verify(ctx, scope); err != nil {
		return OwnedCreateFailure("project-prewrite")
	}
	beforeWrite, err := actions.Inventory(ctx, scope)
	if err != nil || !AvailableForOwnedImport(beforeWrite, alias, fingerprint) {
		return OwnedCreateFailure("inventory-prewrite")
	}
	// Incus's server-side duplicate fingerprint/alias checks are the
	// remaining collision defense against another writer.
	if err := actions.Import(ctx, scope, metadata, rootfs, alias, owner); err != nil {
		return OwnedCreateFailure("import")
	}

	if err := actions.Verify(ctx, scope); err != nil {
		return OwnedCreateFailure("project-postwrite")
	}
	after, err := actions.Inventory(ctx, scope)
	if err != nil || ClassifyOwnerClaim(after, scope, alias, owner) != "claimed" {
		return OwnedCreateFailure("claim-postwrite")
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
		return OwnedCreateFailure("fingerprint-postwrite")
	}
	return nil
}
