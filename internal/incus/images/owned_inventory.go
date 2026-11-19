package images

import (
	"context"
	"encoding/json"
	"fmt"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// Exact-project inventory and image claim evidence never grant authorization.
func InspectOwnedScopedInventory(ctx context.Context, scope incusscope.ProjectScope, inspect func(context.Context, incusscope.ProjectScope) ([]byte, error), maxBytes int) ([]ObservedClaimedImage, error) {
	if inspect == nil {
		return nil, fmt.Errorf("unable to inspect project-local images")
	}
	raw, err := inspect(ctx, scope)
	if err != nil || maxBytes <= 0 || len(raw) > maxBytes {
		return nil, fmt.Errorf("unable to inspect project-local images")
	}
	var images []ObservedClaimedImage
	if err := json.Unmarshal(raw, &images); err != nil || images == nil {
		return nil, fmt.Errorf("invalid project-local image inventory")
	}
	// An incomplete record invalidates the entire snapshot; it must not
	// silently mean an absent target fingerprint or alias.
	for _, image := range images {
		if !OwnedFingerprintPattern.MatchString(image.Fingerprint) {
			return nil, fmt.Errorf("project-local image identity unverified")
		}
	}
	return images, nil
}

func AvailableForOwnedImport(images []ObservedClaimedImage, alias, fingerprint string) bool {
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
