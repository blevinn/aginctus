package imageops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Nix-linked artifacts and alias construction are image operation helpers.
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
		got, err := Artifact(dir, artifact.name, artifact.suffix)
		if err != nil || got != p {
			t.Fatalf("%s got=%q err=%v", artifact.name, got, err)
		}
	}
}

func TestTemporaryImageAliasLengthAndUniqueness(t *testing.T) {
	nonce := "12345-1791752269329409322"
	for _, name := range []string{"a", strings.Repeat("a", 65), strings.Repeat("b", 100)} {
		if !ValidImageName(name) {
			t.Fatalf("test alias is invalid: %q", name)
		}
		stage := TemporaryImageAlias(name, "staged", nonce)
		backup := TemporaryImageAlias(name, "previous", nonce)
		for _, alias := range []string{stage, backup} {
			if !ValidImageName(alias) {
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
	if TemporaryImageAlias(strings.Repeat("a", 100), "staged", nonce) ==
		TemporaryImageAlias(strings.Repeat("a", 100), "staged", nonce+"1") {
		t.Fatal("different nonces generated identical aliases")
	}
}
