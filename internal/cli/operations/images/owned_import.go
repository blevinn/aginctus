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
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "image %q: created with %q claim in project %q\n", alias, owner, scope.Name)
	return 0
}
