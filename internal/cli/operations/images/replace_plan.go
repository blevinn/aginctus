package imageops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// ReplacePlanActions contains only project-local, read-only inputs; this
// operation never authorizes import or replacement of an image.
type ReplacePlanActions struct {
	Verify      func(context.Context, incusscope.ProjectScope) error
	Inventory   func(context.Context, incusscope.ProjectScope) ([]ObservedClaimedImage, error)
	Artifact    func(string, string, string) (string, error)
	Fingerprint func(string, string) (string, error)
	Survey      func(context.Context, string) string
}

// This is a read-only observation, not an authorization result. Even a clean
// survey of the system and current-user managers cannot see other users,
// manually launched image writers, or changes that race with a future write.
type ReplacePlanReport struct {
	Project            string `json:"project"`
	Alias              string `json:"alias"`
	Owner              string `json:"owner"`
	Status             string `json:"status"`
	CurrentFingerprint string `json:"currentFingerprint,omitempty"`
	DesiredFingerprint string `json:"desiredFingerprint,omitempty"`
	KnownControllers   string `json:"knownControllers,omitempty"`
	WriteAuthorized    bool   `json:"writeAuthorized"`
}

func ParseReplacePlanArgs(args []string) (project, owner, alias, dir string, err error) {
	if len(args) != 7 || args[0] != "replace-plan" || args[1] != "--project" ||
		args[3] != "--owner" || (args[4] != "system" && args[4] != "user") ||
		!ValidImageName(args[5]) || args[6] == "" {
		return "", "", "", "", fmt.Errorf("explicit project, system/user owner, alias and artifact directory required")
	}
	if _, err := incusscope.NewProjectScope(args[2]); err != nil {
		return "", "", "", "", err
	}
	return args[2], args[4], args[5], args[6], nil
}

func AliasFingerprint(images []ObservedClaimedImage, name string) string {
	for _, image := range images {
		for _, alias := range image.Aliases {
			if alias.Name == name {
				return image.Fingerprint
			}
		}
	}
	return ""
}

func PlanOwnedReplacement(ctx context.Context, scope incusscope.ProjectScope, owner, alias, desired string, images []ObservedClaimedImage, survey func(context.Context, string) string) ReplacePlanReport {
	plan := ReplacePlanReport{
		Project: scope.Name, Alias: alias, Owner: owner,
		DesiredFingerprint: desired, WriteAuthorized: false,
	}
	claim := ClassifyOwnerClaim(images, scope, alias, owner)
	if claim != "claimed" {
		plan.Status = "blocked-" + claim
		return plan
	}
	plan.CurrentFingerprint = AliasFingerprint(images, alias)
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
	plan.KnownControllers = survey(ctx, owner)
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
func RunReplacePlan(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config, ops ReplacePlanActions) int {
	project, owner, alias, dir, err := ParseReplacePlanArgs(args)
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
	if err := ops.Verify(ctx, scope); err != nil {
		fmt.Fprintln(stderr, "images replace-plan: local project ownership/policy unverified")
		return 1
	}
	images, err := ops.Inventory(ctx, scope)
	if err != nil {
		fmt.Fprintln(stderr, "images replace-plan: image inventory unverified")
		return 1
	}
	metadata, err := ops.Artifact(dir, "metadata", ".tar.xz")
	if err != nil {
		fmt.Fprintln(stderr, "images replace-plan: metadata artifact unavailable")
		return 1
	}
	rootfs, err := ops.Artifact(dir, "rootfs", ".squashfs")
	if err != nil {
		fmt.Fprintln(stderr, "images replace-plan: rootfs artifact unavailable")
		return 1
	}
	fingerprint, err := ops.Fingerprint(metadata, rootfs)
	if err != nil {
		fmt.Fprintln(stderr, "images replace-plan: unable to fingerprint artifacts")
		return 1
	}
	plan := PlanOwnedReplacement(ctx, scope, owner, alias, fingerprint, images, ops.Survey)
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
