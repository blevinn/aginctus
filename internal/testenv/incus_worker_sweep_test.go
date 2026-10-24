package testenv

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
	for _, item := range []struct{name, path string}{
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
	if err := os.Symlink(good, link); err != nil { t.Fatal(err) }
	if _, err := readTrustedWorkerKey(link); err == nil { t.Fatal("symlink credential admitted") }
	if err := os.Chmod(good, 0644); err != nil { t.Fatal(err) }
	if _, err := readTrustedWorkerKey(good); err == nil { t.Fatal("group/world-readable credential admitted") }
	if err := os.Chmod(good, 0600); err != nil { t.Fatal(err) }
	if err := os.WriteFile(good, []byte("too-short"), 0600); err != nil { t.Fatal(err) }
	if _, err := readTrustedWorkerKey(good); err == nil { t.Fatal("short credential admitted") }
	if err := os.WriteFile(good, make([]byte, 33), 0600); err != nil { t.Fatal(err) }
	if _, err := readTrustedWorkerKey(good); err == nil { t.Fatal("oversized credential admitted") }
}

func TestTrustedSweepRefusesInvalidConfigBeforeDaemon(t *testing.T) {
	if os.Geteuid() == 0 { t.Skip("worker process is explicitly non-root") }
	cfg := TrustedIncusSweepOptions{
		SocketPath: "/var/lib/incus/unix.socket",
		StoragePool: "testenv",
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
