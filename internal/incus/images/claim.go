// Package images owns Incus image identity and provenance evidence.
// Self-declared Incus metadata is never an authorization to mutate an image.
package images

import (
	"regexp"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

var OwnedFingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// This is a non-authoritative provenance *claim* recorded as Incus image
// properties, not an attestation or a lock. It can only support diagnostics:
// write authorization must additionally prove the active lifecycle owner
// and check image/alias identity under appropriate concurrency protection.
func ExpectedOwnerClaim(scope incusscope.ProjectScope, alias, owner string) map[string]string {
	return map[string]string{
		"user.aginctus.managed":    "true",
		"user.aginctus.resource":   "image",
		"user.aginctus.deployment": scope.Name,
		"user.aginctus.alias":      alias,
		"user.aginctus.owner":      owner,
	}
}

type ObservedClaimedImage struct {
	Fingerprint string            `json:"fingerprint"`
	Properties  map[string]string `json:"properties"`
	Aliases     []struct {
		Name string `json:"name"`
	} `json:"aliases"`
}

// ClassifyOwnerClaim deliberately never returns "authorized".
// A valid property claim is still self-declared Incus metadata: the actor
// controlling Incus can edit or forge it. Additional aliases imply that
// changing this image could affect a different lifecycle owner.
func ClassifyOwnerClaim(images []ObservedClaimedImage, scope incusscope.ProjectScope, alias, owner string) string {
	var matches []ObservedClaimedImage
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
	if !OwnedFingerprintPattern.MatchString(image.Fingerprint) {
		return "unverified"
	}
	if len(image.Aliases) != 1 {
		return "shared"
	}
	expected := ExpectedOwnerClaim(scope, alias, owner)
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
