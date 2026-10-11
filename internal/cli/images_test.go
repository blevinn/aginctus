package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImagesListAndAlias(t *testing.T) {
	old := runIncusImage
	t.Cleanup(func() { runIncusImage = old })
	runIncusImage = func(_ context.Context, args ...string) ([]byte, error) {
		if strings.Join(args, " ") != "image list --format json" {
			t.Fatalf("args: %v", args)
		}
		return []byte(`[{"fingerprint":"abc","aliases":[{"name":"sample"}]}]`), nil
	}
	for _, cmd := range []string{"list", "ls"} {
		var out, errOut bytes.Buffer
		if got := runImages(context.Background(), []string{cmd}, &out, &errOut); got != 0 || out.String() != "sample\tabc\n" {
			t.Fatalf("%s code=%d stdout=%q stderr=%q", cmd, got, out.String(), errOut.String())
		}
	}
}

func TestImageUpdatePreservesActiveAliasOnFailedImport(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"metadata/image.tar.xz", "rootfs/image.squashfs"} {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	old := runIncusImage
	t.Cleanup(func() { runIncusImage = old })
	var actions []string
	runIncusImage = func(_ context.Context, args ...string) ([]byte, error) {
		actions = append(actions, strings.Join(args, " "))
		if strings.Join(args, " ") == "image list --format json" {
			return []byte("[]"), nil
		}
		return nil, errors.New("simulated failed import")
	}
	if err := updateLocalImage(context.Background(), "working", dir); err == nil {
		t.Fatal("expected failed import")
	}
	if len(actions) != 2 || actions[0] != "image list --format json" || !strings.HasPrefix(actions[1], "image import ") {
		t.Fatalf("unexpected mutating actions: %v", actions)
	}
}

func TestImageUpdateRejectsInvalidInputBeforeIncus(t *testing.T) {
	old := runIncusImage
	t.Cleanup(func() { runIncusImage = old })
	runIncusImage = func(_ context.Context, args ...string) ([]byte, error) {
		t.Fatalf("unexpected Incus call: %v", args)
		return nil, nil
	}
	for _, name := range []string{"../escape", "", "a/b"} {
		if err := updateLocalImage(context.Background(), name, t.TempDir()); err == nil {
			t.Fatalf("accepted name %q", name)
		}
	}
}

func TestImageArtifactResolvesNixSymlinkedFiles(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(t.TempDir(), "store")
	if err := os.MkdirAll(store, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, artifact := range []struct{ name, file, suffix string }{
		{"metadata", "image.tar.xz", ".tar.xz"},
		{"rootfs", "image.squashfs", ".squashfs"},
	} {
		p := filepath.Join(store, artifact.file)
		if err := os.WriteFile(p, []byte("image"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(p, filepath.Join(dir, artifact.name)); err != nil {
			t.Fatal(err)
		}
		got, err := imageArtifact(dir, artifact.name, artifact.suffix)
		if err != nil || got != p {
			t.Fatalf("%s got=%q err=%v", artifact.name, got, err)
		}
	}
}

func TestImageUpdateExistingFingerprintIsNoop(t *testing.T) {
	dir := t.TempDir()
	data := []byte("test-image-data")
	for _, p := range []string{"metadata/image.tar.xz", "rootfs/image.squashfs"} {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil { t.Fatal(err) }
		if err := os.WriteFile(full, data, 0600); err != nil { t.Fatal(err) }
	}
	sum := sha256.Sum256(append(append([]byte{}, data...), data...))
	old := runIncusImage
	t.Cleanup(func() { runIncusImage = old })
	calls := 0
	runIncusImage = func(_ context.Context, args ...string) ([]byte, error) {
		calls++
		if strings.Join(args, " ") != "image list --format json" { t.Fatalf("unexpected mutation: %v", args) }
		return []byte(fmt.Sprintf(`[{"fingerprint":"%x","aliases":[{"name":"working"}]}]`, sum)), nil
	}
	if err := updateLocalImage(context.Background(), "working", dir); err != nil { t.Fatal(err) }
	if calls != 1 { t.Fatalf("expected one read-only Incus call; got %d", calls) }
}

func TestImageUpdateExistingFingerprintCreatesAlias(t *testing.T) {
	dir := t.TempDir()
	data := []byte("image-data")
	for _, p := range []string{"metadata/image.tar.xz", "rootfs/image.squashfs"} {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil { t.Fatal(err) }
		if err := os.WriteFile(full, data, 0600); err != nil { t.Fatal(err) }
	}
	sum := sha256.Sum256(append(append([]byte{}, data...), data...))
	old := runIncusImage
	t.Cleanup(func() { runIncusImage = old })
	var calls []string
	runIncusImage = func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		if strings.Join(args, " ") == "image list --format json" {
			return []byte(fmt.Sprintf(`[{"fingerprint":"%x","aliases":[]}]`, sum)), nil
		}
		return nil, nil
	}
	if err := updateLocalImage(context.Background(), "working", dir); err != nil { t.Fatal(err) }
	if len(calls) != 4 || !strings.HasPrefix(calls[1], "image alias create ") ||
		!strings.HasPrefix(calls[2], "image alias rename ") ||
		!strings.HasPrefix(calls[3], "image alias delete ") {
		t.Fatalf("unexpected actions: %v", calls)
	}
}
