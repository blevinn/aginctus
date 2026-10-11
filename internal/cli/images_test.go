package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
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
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	sum := sha256.Sum256(append(append([]byte{}, data...), data...))
	old := runIncusImage
	t.Cleanup(func() { runIncusImage = old })
	calls := 0
	runIncusImage = func(_ context.Context, args ...string) ([]byte, error) {
		calls++
		if strings.Join(args, " ") != "image list --format json" {
			t.Fatalf("unexpected mutation: %v", args)
		}
		return []byte(fmt.Sprintf(`[{"fingerprint":"%x","aliases":[{"name":"working"}]}]`, sum)), nil
	}
	if err := updateLocalImage(context.Background(), "working", dir); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected one read-only Incus call; got %d", calls)
	}
}

func TestImageUpdateExistingFingerprintCreatesAlias(t *testing.T) {
	dir := t.TempDir()
	data := []byte("image-data")
	for _, p := range []string{"metadata/image.tar.xz", "rootfs/image.squashfs"} {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0600); err != nil {
			t.Fatal(err)
		}
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
	if err := updateLocalImage(context.Background(), "working", dir); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 4 || !strings.HasPrefix(calls[1], "image alias create ") ||
		!strings.HasPrefix(calls[2], "image alias rename ") ||
		!strings.HasPrefix(calls[3], "image alias delete ") {
		t.Fatalf("unexpected actions: %v", calls)
	}
}

func TestTemporaryImageAliasLengthAndUniqueness(t *testing.T) {
	nonce := "12345-1791752269329409322"
	for _, name := range []string{"a", strings.Repeat("a", 65), strings.Repeat("b", 100)} {
		if !validImageName(name) {
			t.Fatalf("test alias is invalid: %q", name)
		}
		stage := temporaryImageAlias(name, "staged", nonce)
		backup := temporaryImageAlias(name, "previous", nonce)
		for _, alias := range []string{stage, backup} {
			if !validImageName(alias) {
				t.Errorf("temporary alias %q is invalid (length %d)", alias, len(alias))
			}
			if !strings.HasSuffix(alias, nonce) {
				t.Errorf("temporary alias %q lost its nonce", alias)
			}
		}
		if stage == backup {
			t.Errorf("staging and backup aliases collided for %q", name)
		}
		if name == "a" && stage != "a-staged-"+nonce {
			t.Errorf("short alias unexpectedly truncated: %q", stage)
		}
	}
	if temporaryImageAlias(strings.Repeat("a", 100), "staged", nonce) ==
		temporaryImageAlias(strings.Repeat("a", 100), "staged", nonce+"1") {
		t.Fatal("different nonces generated identical aliases")
	}
}

func TestImageUpdateRestoresOldAliasWhenPromotionFails(t *testing.T) {
	dir := t.TempDir()
	data := []byte("new-image")
	for _, p := range []string{"metadata/image.tar.xz", "rootfs/image.squashfs"} {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil { t.Fatal(err) }
		if err := os.WriteFile(full, data, 0600); err != nil { t.Fatal(err) }
	}
	sum := sha256.Sum256(append(append([]byte{}, data...), data...))
	old := runIncusImage
	t.Cleanup(func() { runIncusImage = old })
	var actions [][]string
	runIncusImage = func(_ context.Context, args ...string) ([]byte, error) {
		actions = append(actions, append([]string(nil), args...))
		if strings.Join(args, " ") == "image list --format json" {
			return []byte(fmt.Sprintf(`[{"fingerprint":"%x","aliases":[]},{"fingerprint":"old","aliases":[{"name":"working"}]}]`, sum)), nil
		}
		if len(args) == 5 && args[0] == "image" && args[1] == "alias" &&
			args[2] == "rename" && args[4] == "working" &&
			strings.Contains(args[3], "-staged-") {
			return nil, errors.New("promotion blocked")
		}
		return nil, nil
	}
	err := updateLocalImage(context.Background(), "working", dir)
	if err == nil || !strings.Contains(err.Error(), "promote replacement") {
		t.Fatalf("expected promotion failure, got %v", err)
	}
	if len(actions) != 6 {
		t.Fatalf("expected list, stage, backup, promotion, restore, cleanup; got %v", actions)
	}
	stage := actions[1][3]
	backup := actions[2][4]
	if !strings.HasPrefix(stage, "working-staged-") ||
		!strings.HasPrefix(backup, "working-previous-") ||
		strings.Join(actions[2][:3], " ") != "image alias rename" ||
		actions[2][3] != "working" ||
		actions[3][3] != stage || actions[3][4] != "working" ||
		actions[4][3] != backup || actions[4][4] != "working" ||
		strings.Join(actions[5][:3], " ") != "image alias delete" ||
		actions[5][3] != stage {
		t.Fatalf("unexpected rollback sequence: %v", actions)
	}
}
