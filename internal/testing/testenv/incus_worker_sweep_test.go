package testenv

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestTrustedWorkerKeyRejectsSymlinkLoosePermissionsAndWrongSize(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "key")
	if err := os.WriteFile(good, []byte("0123456789abcdef0123456789abcdef"), 0600); err != nil {
		t.Fatal(err)
	}
	key, err := readTrustedWorkerKey(good)
	if err != nil || len(key) != 32 {
		t.Fatalf("trusted private key not accepted: %v", err)
	}
	for _, item := range []struct{ name, path string }{
		{"relative", "key"},
		{"directory", dir},
	} {
		t.Run(item.name, func(t *testing.T) {
			if _, err := readTrustedWorkerKey(item.path); err == nil {
				t.Fatalf("untrusted key path accepted: %q", item.path)
			}
		})
	}
	link := filepath.Join(dir, "symlink")
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readTrustedWorkerKey(link); err == nil {
		t.Fatal("symlink credential admitted")
	}
	if err := os.Chmod(good, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readTrustedWorkerKey(good); err == nil {
		t.Fatal("group/world-readable credential admitted")
	}
	if err := os.Chmod(good, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(good, []byte("too-short"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readTrustedWorkerKey(good); err == nil {
		t.Fatal("short credential admitted")
	}
	if err := os.WriteFile(good, make([]byte, 33), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readTrustedWorkerKey(good); err == nil {
		t.Fatal("oversized credential admitted")
	}
}

func TestTrustedWorkerKeyRejectsFIFOAndHardLinks(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readTrustedWorkerKey(fifo); err == nil {
		t.Fatal("FIFO credential admitted")
	}
	key := filepath.Join(dir, "key")
	if err := os.WriteFile(key, make([]byte, 32), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(key, filepath.Join(dir, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if _, err := readTrustedWorkerKey(key); err == nil {
		t.Fatal("multiply linked credential admitted")
	}
}

func TestTrustedSweepRefusesInvalidConfigBeforeDaemon(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("worker process is explicitly non-root")
	}
	cfg := TrustedIncusSweepOptions{
		SocketPath:    "/var/lib/incus/unix.socket",
		StoragePool:   "testenv",
		PolicyKeyPath: "/does-not-exist/worker.key",
	}
	if _, err := RunTrustedIncusExpirySweep(context.Background(), cfg, time.Time{}); err == nil {
		t.Fatal("zero clock accepted")
	}
	cfg.StoragePool = "../pilot"
	if _, err := RunTrustedIncusExpirySweep(context.Background(), cfg, time.Now()); err == nil {
		t.Fatal("untrusted pool admitted")
	}
	cfg.StoragePool = "testenv"
	cfg.SocketPath = "unix.socket"
	if _, err := RunTrustedIncusExpirySweep(context.Background(), cfg, time.Now()); err == nil {
		t.Fatal("relative socket admitted")
	}
	cfg.SocketPath = "/var/lib/incus/unix.socket"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RunTrustedIncusExpirySweep(ctx, cfg, time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation obscured: %v", err)
	}
}

func TestPrivateWorkerACL(t *testing.T) {
	const uid uint32 = 1234
	const undefinedID = ^uint32(0)
	acl := make([]byte, 44)
	binary.LittleEndian.PutUint32(acl, 2)
	for i, entry := range []struct {
		tag, perm uint16
		id        uint32
	}{
		{1, 4, undefinedID},
		{2, 4, uid},
		{4, 0, undefinedID},
		{16, 4, undefinedID},
		{32, 0, undefinedID},
	} {
		offset := 4 + 8*i
		binary.LittleEndian.PutUint16(acl[offset:], entry.tag)
		binary.LittleEndian.PutUint16(acl[offset+2:], entry.perm)
		binary.LittleEndian.PutUint32(acl[offset+4:], entry.id)
	}
	if !privateWorkerACL(acl, uid) {
		t.Fatal("systemd named-user read-only ACL rejected")
	}
	if privateWorkerACL(acl, uid+1) || privateWorkerACL(acl, 0) ||
		privateWorkerACL(acl[:43], uid) || privateWorkerACL(append(append([]byte(nil), acl...), make([]byte, 8)...), uid) {
		t.Fatal("wrong principal or malformed ACL admitted")
	}
	for _, offset := range []int{0, 6, 14, 22, 30, 38} {
		bad := append([]byte(nil), acl...)
		bad[offset] ^= 2
		if privateWorkerACL(bad, uid) {
			t.Fatalf("unexpected ACL version or permission admitted at offset %d", offset)
		}
	}
}

func TestWorkerSystemdCredentialRejectsArbitraryPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CREDENTIALS_DIRECTORY", dir)
	if isWorkerSystemdCredential(filepath.Join(dir, "policy-key")) {
		t.Fatal("arbitrary credential directory admitted")
	}
	t.Setenv("CREDENTIALS_DIRECTORY", "/run/credentials/aginctus-testenv-acceptance.service")
	if isWorkerSystemdCredential("/run/credentials/aginctus-testenv-other.service/policy-key") ||
		isWorkerSystemdCredential("/run/credentials/aginctus-testenv-acceptance.service/other-key") {
		t.Fatal("wrong unit or credential admitted")
	}
}
