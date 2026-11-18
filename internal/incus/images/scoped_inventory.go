package images

import (
	"context"
	"encoding/json"
	"fmt"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// ScopedAliasFingerprint reads exactly the supplied, project-scoped inventory.
// An inaccessible, oversized, malformed or ambiguous inventory must never be
// interpreted as evidence that an alias is available for creation.
func ScopedAliasFingerprint(ctx context.Context, scope incusscope.ProjectScope, alias string,
	inspect func(context.Context, incusscope.ProjectScope) ([]byte, error), maxBytes int,
) (string, bool, error) {
	if inspect == nil || maxBytes <= 0 {
		return "", false, fmt.Errorf("unable to verify scoped image inventory")
	}
	raw, err := inspect(ctx, scope)
	if err != nil || len(raw) > maxBytes {
		return "", false, fmt.Errorf("unable to verify scoped image inventory")
	}
	var inventory []struct {
		Fingerprint string `json:"fingerprint"`
		Aliases []struct { Name string `json:"name"` } `json:"aliases"`
	}
	if err := json.Unmarshal(raw, &inventory); err != nil || inventory == nil {
		return "", false, fmt.Errorf("invalid scoped image inventory")
	}
	var fingerprint string
	var exists bool
	for _, image := range inventory {
		if image.Fingerprint == "" {
			return "", false, fmt.Errorf("invalid image fingerprint")
		}
		for _, item := range image.Aliases {
			if item.Name != alias {
				continue
			}
			if exists {
				return "", false, fmt.Errorf("ambiguous scoped image alias")
			}
			fingerprint, exists = image.Fingerprint, true
		}
	}
	return fingerprint, exists, nil
}

// VerifyScopedImageProject never treats a project name or image owner label
// as authorization; the caller must supply independently verified policy.
func VerifyScopedImageProject(ctx context.Context, scope incusscope.ProjectScope,
	read func(context.Context, incusscope.ProjectScope) (string, error),
) error {
	if read == nil {
		return fmt.Errorf("local image project ownership and feature policy not verified")
	}
	status, err := read(ctx, scope)
	if err != nil || status != string(incusscope.ProjectCompatible) {
		return fmt.Errorf("local image project ownership and feature policy not verified")
	}
	return nil
}
