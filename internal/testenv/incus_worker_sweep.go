package testenv

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// TrustedIncusSweepOptions are configured ONLY by an installed dedicated
// worker service. Never populate them from a workload checkout or MCP input.
type TrustedIncusSweepOptions struct {
	SocketPath string
	StoragePool string
	PolicyKeyPath string
}

// readTrustedWorkerKey opens the credential without following symlinks and
// verifies the already-open descriptor. It must stay outside /nix/store,
// source control, agent home and any directory writable by the agent.
func readTrustedWorkerKey(path string) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("invalid worker policy credential path")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errors.New("trusted worker credential unavailable")
	}
	file := os.NewFile(uintptr(fd), "worker-policy-key")
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 ||
		info.Size() != 32 {
		return nil, errors.New("trusted worker policy credential has unsafe attributes")
	}
	raw, ok := info.Sys().(*syscall.Stat_t)
	if !ok || raw.Uid != uint32(os.Geteuid()) || raw.Nlink != 1 {
		return nil, errors.New("trusted worker policy credential has unsafe owner")
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(file, key); err != nil {
		return nil, errors.New("trusted worker policy credential unreadable")
	}
	return key, nil
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
	if cfg.SocketPath == "" || !filepath.IsAbs(cfg.SocketPath) || filepath.Clean(cfg.SocketPath) != cfg.SocketPath {
		return empty, errors.New("invalid trusted worker socket configuration")
	}
	if _, err := IncusProjectPolicy(cfg.StoragePool); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	key, err := readTrustedWorkerKey(cfg.PolicyKeyPath)
	if err != nil {
		return empty, err
	}
	defer func() {
		for i := range key { key[i] = 0 }
	}()
	client, err := ConnectTrustedIncusVMClient(ctx, cfg.SocketPath)
	if err != nil {
		return empty, safeIncusError("trusted worker Incus daemon unavailable", err)
	}
	backend, err := NewIncusVMBackend(client, cfg.StoragePool, key)
	if err != nil {
		return empty, err
	}
	return ReconcileExpiredIncusVMs(ctx, backend, now)
}
