package images

import (
	"context"
	"fmt"
	"time"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// CreateActions are explicit project-scoped capabilities. Neither an alias
// nor image metadata grants authorization or permits replacing an image.
type CreateActions struct {
	Lease func(incusscope.ProjectScope) (func(), error)
	Verify func(context.Context, incusscope.ProjectScope) error
	ReadAlias func(context.Context, incusscope.ProjectScope, string) (string, bool, error)
	Artifact func(string, string, string) (string, error)
	Fingerprint func(string, string) (string, error)
	Import func(context.Context, incusscope.ProjectScope, string, string, string) error
}

// CreateScopedImage holds the cooperative writer lease until the final
// independent fingerprint observation. No automatic cleanup follows an
// uncertain import; the server's create-only alias collision is decisive.
func CreateScopedImage(ctx context.Context, scope incusscope.ProjectScope, alias, dir string, actions CreateActions) error {
	if actions.Lease == nil || actions.Verify == nil || actions.ReadAlias == nil ||
		actions.Artifact == nil || actions.Fingerprint == nil || actions.Import == nil {
		return fmt.Errorf("required project-scoped image capability unavailable")
	}
	release, err := actions.Lease(scope)
	if err != nil || release == nil {
		return fmt.Errorf("project writer lease unavailable; no import attempted")
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err := actions.Verify(ctx, scope); err != nil {
		return fmt.Errorf("local project unverified; no import attempted")
	}
	if _, exists, err := actions.ReadAlias(ctx, scope, alias); err != nil || exists {
		return fmt.Errorf("alias already exists or inventory unavailable; refusing adoption")
	}
	metadata, err := actions.Artifact(dir, "metadata", ".tar.xz")
	if err != nil {
		return fmt.Errorf("cannot resolve metadata artifact")
	}
	rootfs, err := actions.Artifact(dir, "rootfs", ".squashfs")
	if err != nil {
		return fmt.Errorf("cannot resolve rootfs artifact")
	}
	fingerprint, err := actions.Fingerprint(metadata, rootfs)
	if err != nil {
		return fmt.Errorf("cannot fingerprint artifacts")
	}
	if err := actions.Verify(ctx, scope); err != nil {
		return fmt.Errorf("local project changed before import; refusing mutation")
	}
	if _, exists, err := actions.ReadAlias(ctx, scope, alias); err != nil || exists {
		return fmt.Errorf("alias changed or inventory unavailable; refusing mutation")
	}
	if err := actions.Import(ctx, scope, metadata, rootfs, alias); err != nil {
		return fmt.Errorf("create-only import refused or failed; inspect the project before retrying")
	}
	if err := actions.Verify(ctx, scope); err != nil {
		return fmt.Errorf("project not verified after import; inspect manually; no automatic rollback")
	}
	got, exists, err := actions.ReadAlias(ctx, scope, alias)
	if err != nil || !exists || got != fingerprint {
		return fmt.Errorf("alias fingerprint not verified after import; inspect manually; no automatic rollback")
	}
	return nil
}
