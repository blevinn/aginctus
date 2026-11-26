package testenv

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageApprovedPayloadBundle(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	valid := bundleFixture(t, "abc", "def")
	header, _, _ := strings.Cut(valid, "\n")
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(header+"\n")))
	for _, invalid := range []string{valid + "extra", valid[:len(valid)-1], bundleFixture(t, "abc", "xyz")} {
		if _, err := StageApprovedPayloadBundle(strings.NewReader(invalid), digest, dir); err == nil {
			t.Fatal("invalid bundle staged")
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			t.Fatalf("failed staging left files: %v %v", entries, err)
		}
	}
	receipt, err := StageApprovedPayloadBundle(strings.NewReader(valid), digest, dir)
	if err != nil || receipt.Status != "staged_integrity_verified" || receipt.SourceAuthority != "not_verified" {
		t.Fatalf("staging: %+v %v", receipt, err)
	}
	path := filepath.Join(dir, digest+".bundle")
	data, err := os.ReadFile(path)
	if err != nil || string(data) != valid {
		t.Fatal("staged bytes differ")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("staged file permissions: %v %v", info, err)
	}
	if _, err := StageApprovedPayloadBundle(strings.NewReader(valid), digest, dir); err == nil {
		t.Fatal("existing stage adopted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("unexpected stage files: %v %v", entries, err)
	}
}

func TestStageApprovedPayloadRejectsUnsafeDirectory(t *testing.T) {
	dir := t.TempDir()
	loose := filepath.Join(dir, "loose")
	if err := os.Mkdir(loose, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"relative", loose, link} {
		if _, err := StageApprovedPayloadBundle(strings.NewReader(""), strings.Repeat("a", 64), target); err == nil {
			t.Fatalf("unsafe staging directory admitted: %s", target)
		}
	}
}
