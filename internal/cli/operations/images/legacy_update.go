package imageops

import (
	"context"

	incusimages "github.com/blevinn/aginctus/internal/incus/images"
)

// LegacyUpdateActions retains the ambient v0.1 signature for CLI callers.
// This intentionally does not use selected-project create-only imports.
type LegacyUpdateActions = incusimages.LegacyUpdateActions

func UpdateLocalImage(ctx context.Context, name, dir string, actions LegacyUpdateActions) error {
	return incusimages.UpdateAmbientImage(ctx, name, dir, actions)
}
