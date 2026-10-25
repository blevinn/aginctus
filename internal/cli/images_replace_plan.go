package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// This is a read-only observation, not an authorization result. Even a clean
// survey of the system and current-user managers cannot see other users,
// manually launched image writers, or changes that race with a future write.
type imageReplacePlanReport struct {
	Project            string `json:"project"`
	Alias              string `json:"alias"`
	Owner              string `json:"owner"`
	Status             string `json:"status"`
	CurrentFingerprint string `json:"currentFingerprint,omitempty"`
	DesiredFingerprint string `json:"desiredFingerprint,omitempty"`
	KnownControllers   string `json:"knownControllers,omitempty"`
	WriteAuthorized    bool   `json:"writeAuthorized"`
}

func parseImageReplacePlanArgs(args []string) (project, owner, alias, dir string, err error) {
	if len(args) != 7 || args[0] != "replace-plan" || args[1] != "--project" ||
		args[3] != "--owner" || (args[4] != "system" && args[4] != "user") ||
		!validImageName(args[5]) || args[6] == "" {
		return "", "", "", "", fmt.Errorf("explicit project, system/user owner, alias and artifact directory required")
	}
	if _, err := incusscope.NewProjectScope(args[2]); err != nil {
		return "", "", "", "", err
	}
	return args[2], args[4], args[5], args[6], nil
}

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

func imageAliasFingerprint(images []observedClaimedImage, name string) string {
	for _, image := range images {
		for _, alias := range image.Aliases {
			if alias.Name == name {
				return image.Fingerprint
			}
		}
	}
	return ""
}

func planOwnedImageReplacement(ctx context.Context, scope incusscope.ProjectScope, owner, alias, desired string, images []observedClaimedImage) imageReplacePlanReport {
	plan := imageReplacePlanReport{
		Project: scope.Name, Alias: alias, Owner: owner,
		DesiredFingerprint: desired, WriteAuthorized: false,
	}
	claim := classifyImageOwnerClaim(images, scope, alias, owner)
	if claim != "claimed" {
		plan.Status = "blocked-" + claim
		return plan
	}
	plan.CurrentFingerprint = imageAliasFingerprint(images, alias)
	if plan.CurrentFingerprint == desired {
		plan.Status = "up-to-date"
		return plan
	}
	for _, image := range images {
		if image.Fingerprint == desired {
			// Existing image content may belong to another alias or owner.
			// Do not import, adopt or rewrite its descriptive properties.
			plan.Status = "blocked-target-fingerprint-present"
			return plan
		}
	}
	plan.KnownControllers = surveyImageControllers(ctx, owner)
	switch plan.KnownControllers {
	case "no-known-conflict":
		// This is only a *candidate plan*, never a green light for writes.
		// Host/other-user authority and concurrency exclusion are not proven.
		plan.Status = "candidate-requires-authorization"
	case "competing-known-unit":
		plan.Status = "blocked-competing-controller"
	default:
		plan.Status = "blocked-controller-unverified"
	}
	return plan
}

// replace-plan never calls an Incus mutation. Errors are deliberately
// sanitized; no raw daemon, systemd, or source filesystem output is echoed.
func runImageReplacePlan(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	project, owner, alias, dir, err := parseImageReplacePlanArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, "Usage: aginctus images replace-plan --project NAME --owner system|user ALIAS PATH")
		return 2
	}
	scope, err := cfg.IncusProjectScope()
	if err != nil || scope.Name != project {
		fmt.Fprintln(stderr, "images replace-plan: requested project differs from incus.project")
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := verifyScopedImageProject(ctx, scope); err != nil {
		fmt.Fprintln(stderr, "images replace-plan: local project ownership/policy unverified")
		return 1
	}
	images, err := inspectOwnedScopedImageInventory(ctx, scope)
	if err != nil {
		fmt.Fprintln(stderr, "images replace-plan: image inventory unverified")
		return 1
	}
	metadata, err := imageArtifact(dir, "metadata", ".tar.xz")
	if err != nil {
		fmt.Fprintln(stderr, "images replace-plan: metadata artifact unavailable")
		return 1
	}
	rootfs, err := imageArtifact(dir, "rootfs", ".squashfs")
	if err != nil {
		fmt.Fprintln(stderr, "images replace-plan: rootfs artifact unavailable")
		return 1
	}
	fingerprint, err := imageFingerprint(metadata, rootfs)
	if err != nil {
		fmt.Fprintln(stderr, "images replace-plan: unable to fingerprint artifacts")
		return 1
	}
	plan := planOwnedImageReplacement(ctx, scope, owner, alias, fingerprint, images)
	if err := json.NewEncoder(stdout).Encode(plan); err != nil {
		fmt.Fprintln(stderr, "images replace-plan: unable to encode plan")
		return 1
	}
	if plan.Status != "up-to-date" && plan.Status != "candidate-requires-authorization" {
		fmt.Fprintln(stderr, "images replace-plan: replacement blocked by observed state; no changes made")
		return 1
	}
	return 0
}
