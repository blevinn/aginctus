package imageops

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// LocalImage is the stable shape of Incus image inventory data.
type LocalImage struct {
	Fingerprint string `json:"fingerprint"`
	Aliases     []struct {
		Name string `json:"name"`
	} `json:"aliases"`
}

// printLocalImages retains the v0.1 tabular contract for both ambient and
// explicitly scoped image inventories; no project identifier is inferred.
func PrintLocalImages(stdout io.Writer, images []LocalImage) {
	for _, img := range images {
		if len(img.Aliases) == 0 {
			fmt.Fprintln(stdout, img.Fingerprint)
			continue
		}
		for _, a := range img.Aliases {
			fmt.Fprintf(stdout, "%s\t%s\n", a.Name, img.Fingerprint)
		}
	}
}

func Artifact(dir, subdir, extension string) (string, error) {
	root, err := filepath.EvalSymlinks(filepath.Join(dir, subdir))
	if err != nil {
		return "", fmt.Errorf("resolve image %s artifact: %w", subdir, err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	// Nix image outputs often expose rootfs and metadata as symlinks directly
	// to store files, not as directories containing those files.
	if info.Mode().IsRegular() {
		if !strings.HasSuffix(root, extension) {
			return "", fmt.Errorf("image %s artifact %q does not end in %s", subdir, root, extension)
		}
		return root, nil
	}
	if !info.IsDir() {
		return "", fmt.Errorf("image %s artifact is neither a file nor a directory", subdir)
	}
	var matches []string
	err = filepath.Walk(root, func(path string, entry os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Mode().IsRegular() && strings.HasSuffix(path, extension) {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("expected exactly one %s artifact under %s, found %d", extension, root, len(matches))
	}
	return matches[0], nil
}

var imageNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)

func ValidImageName(name string) bool {
	return imageNamePattern.MatchString(name)
}

func Fingerprint(metadata, rootfs string) (string, error) {
	hash := sha256.New()
	for _, path := range []string{metadata, rootfs} {
		file, err := os.Open(path)
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return "", fmt.Errorf("hash %s: %w", path, copyErr)
		}
		if closeErr != nil {
			return "", fmt.Errorf("close %s: %w", path, closeErr)
		}
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// temporaryImageAlias preserves the full nonce while keeping Incus aliases
// within the same 100-character limit enforced for user-provided names.
func TemporaryImageAlias(name, role, nonce string) string {
	suffix := "-" + role + "-" + nonce
	maxPrefix := 100 - len(suffix)
	if len(name) > maxPrefix {
		name = name[:maxPrefix]
	}
	return name + suffix
}
