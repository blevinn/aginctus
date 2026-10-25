package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

var ownedImageFingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// This is a non-authoritative provenance *claim* recorded as Incus image
// properties, not an attestation or a lock. It can only support diagnostics:
// write authorization must additionally prove the active lifecycle owner
// and check image/alias identity under appropriate concurrency protection.
func expectedImageOwnerClaim(scope incusscope.ProjectScope, alias, owner string) map[string]string {
	return map[string]string{
		"user.aginctus.managed":    "true",
		"user.aginctus.resource":   "image",
		"user.aginctus.deployment": scope.Name,
		"user.aginctus.alias":      alias,
		"user.aginctus.owner":      owner,
	}
}

type observedClaimedImage struct {
	Fingerprint string            `json:"fingerprint"`
	Properties  map[string]string `json:"properties"`
	Aliases     []struct {
		Name string `json:"name"`
	} `json:"aliases"`
}

type imageOwnerAuditReport struct {
	Project string `json:"project"`
	Alias   string `json:"alias"`
	Owner   string `json:"owner"`
	Status  string `json:"status"`
}

// classifyImageOwnerClaim deliberately never returns "authorized".
// A valid property claim is still self-declared Incus metadata: the actor
// controlling Incus can edit or forge it. Additional aliases imply that
// changing this image could affect a different lifecycle owner.
func classifyImageOwnerClaim(images []observedClaimedImage, scope incusscope.ProjectScope, alias, owner string) string {
	var matches []observedClaimedImage
	occurrences := 0
	for _, image := range images {
		for _, item := range image.Aliases {
			if item.Name == alias {
				occurrences++
				matches = append(matches, image)
			}
		}
	}
	if occurrences == 0 {
		return "absent"
	}
	if occurrences != 1 {
		return "ambiguous"
	}
	image := matches[0]
	if !ownedImageFingerprintPattern.MatchString(image.Fingerprint) {
		return "unverified"
	}
	if len(image.Aliases) != 1 {
		return "shared"
	}
	expected := expectedImageOwnerClaim(scope, alias, owner)
	marked := false
	for _, key := range []string{
		"user.aginctus.managed", "user.aginctus.resource",
		"user.aginctus.deployment", "user.aginctus.alias", "user.aginctus.owner",
	} {
		if image.Properties[key] != "" {
			marked = true
		}
	}
	if !marked {
		return "unmarked"
	}
	for key, value := range expected {
		if image.Properties[key] != value {
			return "foreign-or-incomplete"
		}
	}
	return "claimed"
}

func parseImageOwnerAuditArgs(args []string) (project, owner, alias string, err error) {
	if len(args) != 6 || args[0] != "owner-audit" || args[1] != "--project" ||
		args[3] != "--owner" || (args[4] != "system" && args[4] != "user") ||
		!validImageName(args[5]) {
		return "", "", "", fmt.Errorf("explicit project, selected owner and alias required")
	}
	if _, err := incusscope.NewProjectScope(args[2]); err != nil {
		return "", "", "", err
	}
	return args[2], args[4], args[5], nil
}

// Only read local daemon state with an exact named project and no implicit
// remote/default fallback. Failures never expose Incus error/output text.
func runImageOwnerAudit(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	project, owner, alias, err := parseImageOwnerAuditArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, "Usage: aginctus images owner-audit --project NAME --owner system|user ALIAS")
		return 2
	}
	scope, err := cfg.IncusProjectScope()
	if err != nil || scope.Name != project {
		fmt.Fprintln(stderr, "images owner-audit: requested project differs from incus.project")
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := verifyScopedImageProject(ctx, scope); err != nil {
		fmt.Fprintln(stderr, "images owner-audit: project ownership/policy unverified; no image read attempted")
		return 1
	}
	raw, err := inspectScopedImages(ctx, scope)
	if err != nil || len(raw) > maxProjectListBytes {
		fmt.Fprintln(stderr, "images owner-audit: scoped image inventory unavailable")
		return 1
	}
	var images []observedClaimedImage
	if err := json.Unmarshal(raw, &images); err != nil || images == nil {
		fmt.Fprintln(stderr, "images owner-audit: invalid scoped image inventory")
		return 1
	}
	state := classifyImageOwnerClaim(images, scope, alias, owner)
	if err := json.NewEncoder(stdout).Encode(imageOwnerAuditReport{
		Project: scope.Name, Alias: alias, Owner: owner, Status: state,
	}); err != nil {
		fmt.Fprintln(stderr, "images owner-audit: unable to write report")
		return 1
	}
	if state != "claimed" {
		fmt.Fprintln(stderr, "images owner-audit: no complete exclusive ownership claim; no changes made")
		return 1
	}
	return 0
}
