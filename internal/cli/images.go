package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

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
		return imageops.ListLocalImages(ctx, stdout, stderr, runIncusImage)
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
	return imageops.LocalImages(ctx, runIncusImage)
}

func temporaryImageAlias(name, role, nonce string) string {
	return imageops.TemporaryImageAlias(name, role, nonce)
}

func updateLocalImage(ctx context.Context, name, dir string) error {
	return imageops.UpdateLocalImage(ctx, name, dir, imageops.LegacyUpdateActions{
		Run: runIncusImage,
		Artifact: imageArtifact,
		Fingerprint: imageFingerprint,
		LocalImages: localImages,
	})
}
