package imageops

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
	incusimages "github.com/blevinn/aginctus/internal/incus/images"
)

// OwnedImportActions explicitly passes the project lease, authoritative
// local observations and create-only Incus operation to the application
// layer. The owner marker is diagnostic, not a write authorization.
type OwnedImportActions = incusimages.OwnedCreateActions

func ScopedOwnedImageImportArgs(scope incusscope.ProjectScope, metadata, rootfs, alias, owner string) []string {
	props := ExpectedOwnerClaim(scope, alias, owner)
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

func ParseScopedOwnedImageImportArgs(args []string) (project, owner, alias, dir string, err error) {
	if len(args) != 8 || args[0] != "import-owned" || args[1] != "--project" ||
		args[3] != "--owner" || (args[4] != "system" && args[4] != "user") ||
		args[7] != "--confirm-create" || !ValidImageName(args[5]) || args[6] == "" {
		return "", "", "", "", fmt.Errorf("explicit project, system/user claim, alias, artifact path and confirmation required")
	}
	if _, err := incusscope.NewProjectScope(args[2]); err != nil {
		return "", "", "", "", err
	}
	return args[2], args[4], args[5], args[6], nil
}

// Domain-owned compatibility adapters for CLI callers.
func InspectOwnedScopedImageInventory(ctx context.Context, scope incusscope.ProjectScope, inspect func(context.Context, incusscope.ProjectScope) ([]byte, error), maxBytes int) ([]ObservedClaimedImage, error) {
 return incusimages.InspectOwnedScopedInventory(ctx, scope, inspect, maxBytes)
}
func AvailableForOwnedImport(images []ObservedClaimedImage, alias, fingerprint string) bool {
 return incusimages.AvailableForOwnedImport(images, alias, fingerprint)
}

// ownedCreateDiagnostic is CLI presentation, never a domain authority check.
func ownedCreateDiagnostic(err error) string {
	switch err {
	case incusimages.OwnedCreateFailure("selection"):
		return "images import-owned: invalid project, owner or alias; no import attempted"
	case incusimages.OwnedCreateFailure("capabilities"):
		return "images import-owned: required capabilities unavailable; no import attempted"
	case incusimages.OwnedCreateFailure("lease"):
		return "images import-owned: project writer lease unavailable; no import attempted"
	case incusimages.OwnedCreateFailure("project-initial"):
		return "images import-owned: local project not verified; no import attempted"
	case incusimages.OwnedCreateFailure("inventory-initial"):
		return "images import-owned: cannot verify project-local image inventory"
	case incusimages.OwnedCreateFailure("metadata"):
		return "images import-owned: cannot resolve metadata artifact"
	case incusimages.OwnedCreateFailure("rootfs"):
		return "images import-owned: cannot resolve rootfs artifact"
	case incusimages.OwnedCreateFailure("fingerprint"):
		return "images import-owned: cannot fingerprint image artifacts"
	case incusimages.OwnedCreateFailure("adoption"):
		return "images import-owned: alias or fingerprint already exists; refusing adoption"
	case incusimages.OwnedCreateFailure("project-prewrite"):
		return "images import-owned: project changed before import; no mutation attempted"
	case incusimages.OwnedCreateFailure("inventory-prewrite"):
		return "images import-owned: image inventory changed or unavailable; refusing mutation"
	case incusimages.OwnedCreateFailure("import"):
		return "images import-owned: creation refused or failed; inspect state manually before retrying"
	case incusimages.OwnedCreateFailure("project-postwrite"):
		return "images import-owned: project unverified after creation; no automatic rollback"
	case incusimages.OwnedCreateFailure("claim-postwrite"):
		return "images import-owned: ownership claim not verified after creation; no automatic rollback"
	case incusimages.OwnedCreateFailure("fingerprint-postwrite"):
		return "images import-owned: fingerprint mismatch after creation; no automatic rollback"
	default:
		return "images import-owned: creation failed; inspect project state before retrying"
	}
}

// This command creates a fresh image record with a *self-declared* owner
// claim. Neither the owner flag nor the properties authenticate the caller
// or prove an active systemd/Home Manager controller. As with the older
// create-only import, do not automatically delete uncertain resources.
func RunScopedOwnedImageImport(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config, actions OwnedImportActions) int {
	project, owner, alias, dir, err := ParseScopedOwnedImageImportArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, "Usage: aginctus images import-owned --project NAME --owner system|user ALIAS PATH --confirm-create")
		return 2
	}
	scope, err := cfg.IncusProjectScope()
	if err != nil || scope.Name != project {
		fmt.Fprintln(stderr, "images import-owned: project must match the declared incus.project setting")
		return 2
	}
	if err := incusimages.CreateClaimedImage(ctx, scope, owner, alias, dir, actions); err != nil {
		fmt.Fprintln(stderr, ownedCreateDiagnostic(err))
		return 1
	}
	fmt.Fprintf(stdout, "image %q: created with %q claim in project %q\n", alias, owner, scope.Name)
	return 0
}
