package imageops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
	incusimages "github.com/blevinn/aginctus/internal/incus/images"
)

// Compatibility shims for existing CLI call sites. Image ownership is
// diagnostic resource evidence owned by incus/images, never authorization.
var OwnedFingerprintPattern = incusimages.OwnedFingerprintPattern
type ObservedClaimedImage = incusimages.ObservedClaimedImage

func ExpectedOwnerClaim(scope incusscope.ProjectScope, alias, owner string) map[string]string {
	return incusimages.ExpectedOwnerClaim(scope, alias, owner)
}

func ClassifyOwnerClaim(images []ObservedClaimedImage, scope incusscope.ProjectScope, alias, owner string) string {
	return incusimages.ClassifyOwnerClaim(images, scope, alias, owner)
}

type OwnerAuditReport struct {
	Project string `json:"project"`
	Alias   string `json:"alias"`
	Owner   string `json:"owner"`
	Status  string `json:"status"`
}

func ParseOwnerAuditArgs(args []string) (project, owner, alias string, err error) {
	if len(args) != 6 || args[0] != "owner-audit" || args[1] != "--project" ||
		args[3] != "--owner" || (args[4] != "system" && args[4] != "user") ||
		!ValidImageName(args[5]) {
		return "", "", "", fmt.Errorf("explicit project, selected owner and alias required")
	}
	if _, err := incusscope.NewProjectScope(args[2]); err != nil {
		return "", "", "", err
	}
	return args[2], args[4], args[5], nil
}

// Only read local daemon state with an exact named project and no implicit
// remote/default fallback. Failures never expose Incus error/output text.
func RunOwnerAudit(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config, verify func(context.Context, incusscope.ProjectScope) error, inspect func(context.Context, incusscope.ProjectScope) ([]byte, error), maxBytes int) int {
	project, owner, alias, err := ParseOwnerAuditArgs(args)
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
	if err := verify(ctx, scope); err != nil {
		fmt.Fprintln(stderr, "images owner-audit: project ownership/policy unverified; no image read attempted")
		return 1
	}
	raw, err := inspect(ctx, scope)
	if err != nil || len(raw) > maxBytes {
		fmt.Fprintln(stderr, "images owner-audit: scoped image inventory unavailable")
		return 1
	}
	var images []ObservedClaimedImage
	if err := json.Unmarshal(raw, &images); err != nil || images == nil {
		fmt.Fprintln(stderr, "images owner-audit: invalid scoped image inventory")
		return 1
	}
	state := ClassifyOwnerClaim(images, scope, alias, owner)
	if err := json.NewEncoder(stdout).Encode(OwnerAuditReport{
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
