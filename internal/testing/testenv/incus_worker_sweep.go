package testenv

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const workerRecoveryLedgerDir = "/var/lib/aginctus-testenv-recovery"

// TrustedIncusSweepOptions are configured ONLY by an installed dedicated
// worker service. Never populate them from a workload checkout or MCP input.
type TrustedIncusSweepOptions struct {
	SocketPath    string
	StoragePool   string
	PolicyKeyPath string
}

// readTrustedWorkerKey opens the credential without following symlinks and
// verifies the already-open descriptor. It must stay outside /nix/store,
// source control, agent home and any directory writable by the agent.
func readTrustedWorkerKey(path string) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("invalid worker policy credential path")
	}
	// Nonblocking open lets the descriptor check reject FIFOs without waiting for a writer.
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errors.New("trusted worker credential unavailable")
	}
	file := os.NewFile(uintptr(fd), "worker-policy-key")
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != 32 {
		return nil, errors.New("trusted worker policy credential has unsafe attributes")
	}
	raw, ok := info.Sys().(*syscall.Stat_t)
	if !ok || raw.Nlink != 1 {
		return nil, errors.New("trusted worker policy credential has unsafe owner")
	}
	uid := uint32(os.Geteuid())
	// systemd prefers a root-owned read-only credential with a named-user
	// ACL; the group mode bits then represent the ACL mask, not group access.
	// Keep ordinary file admission strict and admit only that exact ACL.
	if raw.Uid != uid || info.Mode().Perm()&0077 != 0 {
		if raw.Uid != 0 || uid == 0 || info.Mode().Perm() != 0440 ||
			!isWorkerSystemdCredential(path) || !hasPrivateWorkerACL(fd, uid) {
			return nil, errors.New("trusted worker policy credential has unsafe owner or permissions")
		}
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(file, key); err != nil {
		clear(key)
		return nil, errors.New("trusted worker policy credential unreadable")
	}
	return key, nil
}

// isWorkerSystemdCredential limits ACL-backed admission to the current unit's
// systemd credential directory. /run/credentials is a trusted system boundary.
func isWorkerSystemdCredential(path string) bool {
	dir := filepath.Dir(path)
	unit := filepath.Base(dir)
	if filepath.Base(path) != "policy-key" || filepath.Dir(dir) != "/run/credentials" ||
		dir != os.Getenv("CREDENTIALS_DIRECTORY") ||
		!strings.HasPrefix(unit, "aginctus-testenv-") || !strings.HasSuffix(unit, ".service") {
		return false
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
		return false
	}
	raw, ok := info.Sys().(*syscall.Stat_t)
	return ok && raw.Uid == 0
}

// Linux POSIX ACL xattrs have a little-endian version header and fixed
// 8-byte entries. Require exactly owner read, service UID read, no owning
// group permissions, read-only mask, and no other permissions. Reject
// additional principals, malformed or unavailable ACLs, and writable masks.
func hasPrivateWorkerACL(fd int, uid uint32) bool {
	var acl [44]byte
	n, err := unix.Fgetxattr(fd, "system.posix_acl_access", acl[:])
	return err == nil && n == len(acl) && privateWorkerACL(acl[:], uid)
}

func privateWorkerACL(acl []byte, uid uint32) bool {
	if uid == 0 || len(acl) != 44 || binary.LittleEndian.Uint32(acl[:4]) != 2 {
		return false
	}
	const undefinedID = ^uint32(0)
	expected := [...]struct {
		tag, perm uint16
		id        uint32
	}{
		{0x01, 4, undefinedID}, // ACL_USER_OBJ
		{0x02, 4, uid},         // ACL_USER
		{0x04, 0, undefinedID}, // ACL_GROUP_OBJ
		{0x10, 4, undefinedID}, // ACL_MASK
		{0x20, 0, undefinedID}, // ACL_OTHER
	}
	for i, entry := range expected {
		offset := 4 + 8*i
		if binary.LittleEndian.Uint16(acl[offset:]) != entry.tag ||
			binary.LittleEndian.Uint16(acl[offset+2:]) != entry.perm ||
			binary.LittleEndian.Uint32(acl[offset+4:]) != entry.id {
			return false
		}
	}
	return true
}

// RunTrustedIncusExpirySweep performs one externally scheduled reconciliation.
// This library never starts background processes, creates projects, or grants
// daemon access. The NixOS service must confine it to the dedicated worker.
func RunTrustedIncusExpirySweep(ctx context.Context, cfg TrustedIncusSweepOptions, now time.Time) (IncusReconcileResult, error) {
	var empty IncusReconcileResult
	if os.Geteuid() == 0 {
		return empty, errors.New("testenv recovery must run as a dedicated non-root worker")
	}
	if now.IsZero() {
		return empty, errors.New("trusted reconciliation clock required")
	}
	if cfg.SocketPath != "/var/lib/incus/unix.socket" {
		return empty, errors.New("invalid trusted worker socket configuration")
	}
	if cfg.StoragePool != "testenv" {
		return empty, errors.New("invalid trusted worker pool configuration")
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	key, err := readTrustedWorkerKey(cfg.PolicyKeyPath)
	if err != nil {
		return empty, err
	}
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()
	client, err := ConnectTrustedIncusVMClient(ctx, cfg.SocketPath)
	if err != nil {
		return empty, safeIncusError("trusted worker Incus daemon unavailable", err)
	}
	if _, ok := client.(incusRootVolumeInventory); !ok {
		return empty, ErrUnsupportedAction
	}
	backend, err := NewIncusVMBackend(client, cfg.StoragePool, key)
	if err != nil {
		return empty, err
	}
	defer clear(backend.policyKey)
	return ReconcileExpiredIncusVMsDurable(ctx, backend, now, workerRecoveryLedgerDir)
}
