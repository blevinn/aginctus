package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	imageops "github.com/blevinn/aginctus/internal/cli/operations/images"
)

type imageRunner func(context.Context, ...string) ([]byte, error)

var runIncusImage imageRunner = func(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "incus", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("incus %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

// Local CLI tests and callers retain these names while the artifact,
// identity, and presentation rules live in a dedicated operation package.
type localImage = imageops.LocalImage

func printLocalImages(stdout io.Writer, images []localImage) {
	imageops.PrintLocalImages(stdout, images)
}

func imageArtifact(dir, subdir, extension string) (string, error) {
	return imageops.Artifact(dir, subdir, extension)
}

func validImageName(name string) bool { return imageops.ValidImageName(name) }

func imageFingerprint(metadata, rootfs string) (string, error) {
	return imageops.Fingerprint(metadata, rootfs)
}

func runImages(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "list" || args[0] == "ls") {
		output, err := runIncusImage(ctx, "image", "list", "--format", "json")
		if err != nil {
			fmt.Fprintf(stderr, "list images: %v\n", err)
			return 1
		}
		var images []localImage
		if err := json.Unmarshal(output, &images); err != nil {
			fmt.Fprintf(stderr, "parse Incus image list: %v\n", err)
			return 1
		}
		printLocalImages(stdout, images)
		return 0
	}
	if len(args) == 3 && args[0] == "update" {
		if err := updateLocalImage(ctx, args[1], args[2]); err != nil {
			fmt.Fprintf(stderr, "update image: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "image %q: updated\n", args[1])
		return 0
	}
	fmt.Fprintln(stderr, "Usage: aginctus images list|ls")
	fmt.Fprintln(stderr, "       aginctus images update <name> <path>")
	return 2
}

func localImages(ctx context.Context) ([]localImage, error) {
	out, err := runIncusImage(ctx, "image", "list", "--format", "json")
	if err != nil {
		return nil, err
	}
	var images []localImage
	if err := json.Unmarshal(out, &images); err != nil {
		return nil, fmt.Errorf("parse Incus image list: %w", err)
	}
	return images, nil
}

func temporaryImageAlias(name, role, nonce string) string {
	return imageops.TemporaryImageAlias(name, role, nonce)
}

func updateLocalImage(ctx context.Context, name, dir string) error {
	if !validImageName(name) {
		return fmt.Errorf("invalid image alias %q", name)
	}
	metadata, err := imageArtifact(dir, "metadata", ".tar.xz")
	if err != nil {
		return err
	}
	rootfs, err := imageArtifact(dir, "rootfs", ".squashfs")
	if err != nil {
		return err
	}
	fingerprint, err := imageFingerprint(metadata, rootfs)
	if err != nil {
		return fmt.Errorf("fingerprint image: %w", err)
	}
	images, err := localImages(ctx)
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
	stage := temporaryImageAlias(name, "staged", nonce)
	backup := temporaryImageAlias(name, "previous", nonce)
	if present {
		_, err = runIncusImage(ctx, "image", "alias", "create", stage, fingerprint)
	} else {
		_, err = runIncusImage(ctx, "image", "import", metadata, rootfs, "--alias", stage)
	}
	if err != nil {
		return fmt.Errorf("stage replacement: %w", err)
	}
	defer func() { _, _ = runIncusImage(context.Background(), "image", "alias", "delete", stage) }()
	moved := false
	if exists {
		if _, err = runIncusImage(ctx, "image", "alias", "rename", name, backup); err != nil {
			return fmt.Errorf("preserve existing alias: %w", err)
		}
		moved = true
	}
	if _, err = runIncusImage(ctx, "image", "alias", "rename", stage, name); err != nil {
		if moved {
			if _, restoreErr := runIncusImage(context.Background(), "image", "alias", "rename", backup, name); restoreErr != nil {
				return errors.Join(fmt.Errorf("promote replacement: %w", err), fmt.Errorf("rollback failed; old image remains under %s: %w", backup, restoreErr))
			}
		}
		return fmt.Errorf("promote replacement: %w", err)
	}
	if moved {
		if _, err = runIncusImage(ctx, "image", "alias", "delete", backup); err != nil {
			return fmt.Errorf("replacement active, but previous alias cleanup failed: %w", err)
		}
	}
	return nil
}
