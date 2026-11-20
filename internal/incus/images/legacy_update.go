package images

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

// LocalImage is the minimal shape returned by the legacy ambient Incus image
// listing. Legacy operations intentionally do not select an exact project.
type LocalImage struct {
	Fingerprint string `json:"fingerprint"`
	Aliases     []struct {
		Name string `json:"name"`
	} `json:"aliases"`
}

// LegacyUpdateActions preserve the v0.1 ambient Incus runner contract;
// they never acquire scoped resource authority from alias claims.
type LegacyUpdateActions struct {
	Run         func(context.Context, ...string) ([]byte, error)
	Artifact    func(string, string, string) (string, error)
	Fingerprint func(string, string) (string, error)
	LocalImages func(context.Context) ([]LocalImage, error)
}

// UpdateLocalImage retains the existing v0.1 ambient-context alias staging,
// promotion and best-effort rollback sequence. It intentionally does not
// import the scoped API's project selection or claim-based authorization.
func UpdateAmbientImage(ctx context.Context, name, dir string, actions LegacyUpdateActions) error {
	if !ValidImageName(name) {
		return fmt.Errorf("invalid image alias %q", name)
	}
	metadata, err := actions.Artifact(dir, "metadata", ".tar.xz")
	if err != nil {
		return err
	}
	rootfs, err := actions.Artifact(dir, "rootfs", ".squashfs")
	if err != nil {
		return err
	}
	fingerprint, err := actions.Fingerprint(metadata, rootfs)
	if err != nil {
		return fmt.Errorf("fingerprint image: %w", err)
	}
	images, err := actions.LocalImages(ctx)
	if err != nil {
		return fmt.Errorf("inspect existing images: %w", err)
	}
	present := false
	exists := false
	for _, image := range images {
		if image.Fingerprint == fingerprint {
			present = true
		}
		for _, alias := range image.Aliases {
			if alias.Name == name {
				exists = true
				if image.Fingerprint == fingerprint {
					return nil // The intended image is already active.
				}
			}
		}
	}
	nonce := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	stage := TemporaryImageAlias(name, "staged", nonce)
	backup := TemporaryImageAlias(name, "previous", nonce)
	if present {
		_, err = actions.Run(ctx, "image", "alias", "create", stage, fingerprint)
	} else {
		_, err = actions.Run(ctx, "image", "import", metadata, rootfs, "--alias", stage)
	}
	if err != nil {
		return fmt.Errorf("stage replacement: %w", err)
	}
	defer func() { _, _ = actions.Run(context.Background(), "image", "alias", "delete", stage) }()
	moved := false
	if exists {
		if _, err = actions.Run(ctx, "image", "alias", "rename", name, backup); err != nil {
			return fmt.Errorf("preserve existing alias: %w", err)
		}
		moved = true
	}
	if _, err = actions.Run(ctx, "image", "alias", "rename", stage, name); err != nil {
		if moved {
			if _, restoreErr := actions.Run(context.Background(), "image", "alias", "rename", backup, name); restoreErr != nil {
				return errors.Join(fmt.Errorf("promote replacement: %w", err), fmt.Errorf("rollback failed; old image remains under %s: %w", backup, restoreErr))
			}
		}
		return fmt.Errorf("promote replacement: %w", err)
	}
	if moved {
		if _, err = actions.Run(ctx, "image", "alias", "delete", backup); err != nil {
			return fmt.Errorf("replacement active, but previous alias cleanup failed: %w", err)
		}
	}
	return nil
}
