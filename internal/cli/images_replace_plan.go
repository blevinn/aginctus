package cli

import (
	"context"
	"io"

	imageops "github.com/blevinn/aginctus/internal/cli/operations/images"
	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// Keep the unit/controller observation bridge here until the privileged
// operator evidence implementation is separately extracted.
// No known conflict is NOT evidence of exclusive authority: every call is
// bounded to the system and current user's known four lifecycle units.
func surveyImageControllers(ctx context.Context, owner string) string {
	ownerSeen := false
	conflict, unknown := false, false
	for _, unit := range operatorLifecycleUnits {
		system := inspectOperatorUnitEvidence(ctx, "system", unit)
		user := inspectOperatorUnitEvidence(ctx, "user", unit)
		if system == unitUnknown || user == unitUnknown {
			unknown = true
		}
		selected, other := system, user
		if owner == "user" {
			selected, other = user, system
		}
		if other == unitActive || other == unitPresent {
			conflict = true
		}
		if selected == unitActive || selected == unitPresent {
			ownerSeen = true
		}
	}
	switch {
	case conflict:
		return "competing-known-unit"
	case unknown:
		return "unverified-unit-manager"
	case !ownerSeen:
		return "selected-owner-unit-not-observed"
	default:
		return "no-known-conflict"
	}
}

type imageReplacePlanReport = imageops.ReplacePlanReport

func parseImageReplacePlanArgs(args []string) (string, string, string, string, error) {
	return imageops.ParseReplacePlanArgs(args)
}

func imageAliasFingerprint(images []observedClaimedImage, name string) string {
	return imageops.AliasFingerprint(images, name)
}

func planOwnedImageReplacement(ctx context.Context, scope incusscope.ProjectScope, owner, alias, desired string, images []observedClaimedImage) imageReplacePlanReport {
	return imageops.PlanOwnedReplacement(ctx, scope, owner, alias, desired, images, surveyImageControllers)
}

func runImageReplacePlan(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	return imageops.RunReplacePlan(ctx, args, stdout, stderr, cfg, imageops.ReplacePlanActions{
		Verify:      verifyScopedImageProject,
		Inventory:   inspectOwnedScopedImageInventory,
		Artifact:    imageArtifact,
		Fingerprint: imageFingerprint,
		Survey:      surveyImageControllers,
	})
}
