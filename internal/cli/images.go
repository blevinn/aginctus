package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
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

type localImage struct {
	Fingerprint string `json:"fingerprint"`
	Aliases     []struct {
		Name string `json:"name"`
	} `json:"aliases"`
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
		for _, img := range images {
			if len(img.Aliases) == 0 {
				fmt.Fprintln(stdout, img.Fingerprint)
				continue
			}
			for _, a := range img.Aliases {
				fmt.Fprintf(stdout, "%s\t%s\n", a.Name, img.Fingerprint)
			}
		}
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

func imageArtifact(dir, subdir, extension string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, subdir, "*"+extension))
	if err != nil {
		return "", err
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("expected exactly one %s artifact in %s", extension, filepath.Join(dir, subdir))
	}
	info, err := os.Stat(matches[0])
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("image artifact is not a regular file")
	}
	return matches[0], nil
}

var imageNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)

func validImageName(name string) bool {
	return imageNamePattern.MatchString(name)
}

func imageAliasExists(ctx context.Context, name string) (bool, error) {
	out, err := runIncusImage(ctx, "image", "list", "--format", "json")
	if err != nil {
		return false, err
	}
	var images []localImage
	if err := json.Unmarshal(out, &images); err != nil {
		return false, err
	}
	for _, img := range images {
		for _, a := range img.Aliases {
			if a.Name == name {
				return true, nil
			}
		}
	}
	return false, nil
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
	nonce := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	stage := name + "-staged-" + nonce
	backup := name + "-previous-" + nonce
	if _, err = runIncusImage(ctx, "image", "import", metadata, rootfs, "--alias", stage); err != nil {
		return fmt.Errorf("import replacement: %w", err)
	}
	defer func() { _, _ = runIncusImage(context.Background(), "image", "alias", "delete", stage) }()
	exists, err := imageAliasExists(ctx, name)
	if err != nil {
		return err
	}
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
