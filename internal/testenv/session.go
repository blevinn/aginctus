package testenv

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	sessionShutdownGrace = 2 * time.Second
	sessionSocketID      = "aginctus-qga"
)

var ErrSessionExpired = errors.New("VM session lifetime expired")

// SessionOutcome is the supervisor's observation of its direct child. It
// deliberately does NOT attest cgroup teardown, descendants or disk cleanup.
type SessionOutcome struct {
	ID            string    `json:"run_id"`
	EndedAt       time.Time `json:"ended_at"`
	Reason        string    `json:"reason"`
	ProcessReaped bool      `json:"process_reaped"`
	Cgroup        string    `json:"cgroup"`
	Cleanup       string    `json:"cleanup"`
	Upstream      string    `json:"upstream_inference"`
}

// validatePinnedVM verifies the immutable binary selected by TRUSTED worker
// configuration, not by a workspace, MCP tool argument or model output.
func validatePinnedVM(launcher string) error {
	if !filepath.IsAbs(launcher) || filepath.Clean(launcher) != launcher ||
		!strings.HasPrefix(launcher, "/nix/store/") {
		return errors.New("VM launcher must be a pinned Nix store path")
	}
	resolved, err := filepath.EvalSymlinks(launcher)
	if err != nil || !strings.HasPrefix(resolved, "/nix/store/") {
		return errors.New("VM launcher resolves outside the Nix store")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return errors.New("VM launcher must be an executable store file")
	}
	return nil
}

// vmLaunchArgs contains the ONLY extra QEMU flags we permit. The NixOS
// launcher itself is built with -nic none, no host store mounts, fixed
// vCPU/RAM and a guest-local store image. No caller-provided QEMU args.
func vmLaunchArgs(socket string) []string {
	return []string{
		"-chardev", "socket,id=" + sessionSocketID + ",path=" + socket + ",server=on,wait=off",
		"-device", "virtio-serial-pci",
		"-device", "virtserialport,chardev=" + sessionSocketID + ",name=org.qemu.guest_agent.0",
	}
}

func vmLaunchEnvironment(runDir string) []string {
	// No inherited HOME/SSH_AUTH_SOCK, QEMU_OPTS, NIX_PATH, INCUS_*
	// variables, Nix build hooks, live acceptance flags, provider tokens
	// or extra daemon sockets. The runner script has absolute Nix paths.
	return []string{
		"HOME=" + runDir,
		"PATH=/run/current-system/sw/bin:/usr/bin:/bin",
		"LANG=C",
		"TZ=UTC",
		"TMPDIR=" + filepath.Join(runDir, "tmp"),
		"USE_TMPDIR=1",
		"NIX_DISK_IMAGE=" + filepath.Join(runDir, "root.qcow2"),
	}
}

// reserveVMRun creates the only writable host paths the generated Nix VM
// script may access. The private exchange directory is NOT a host grant to
// arbitrary filesystem paths, but still must be treated as shared guest data.
func (s *RunStore) reserveVMRun() (string, string, error) {
	var token [12]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", "", err
	}
	id := runIDPrefix + hex.EncodeToString(token[:])
	dir := filepath.Join(s.root, id)
	if err := os.Mkdir(dir, 0700); err != nil {
		return "", "", fmt.Errorf("reserve VM run: %w", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "tmp"), 0700); err != nil {
		return "", "", errors.New("cannot create private VM temporary directory")
	}
	return id, dir, nil
}

// bindVMRun makes the session available to the existing Status/ExecGuest
// owner checks after the process is started. Startup failures leave an
// inspectable private directory instead of attempting broad cleanup.
func (s *RunStore) bindVMRun(id string, pid int, ttl time.Duration) (RunRecord, error) {
	if !validRunID(id) || ttl <= 0 || ttl > maxRunLifetime {
		return RunRecord{}, errors.New("invalid reserved VM session")
	}
	dir := filepath.Join(s.root, id)
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return RunRecord{}, errors.New("reserved VM directory invalid")
	}
	if err := ownedByWorker(info); err != nil {
		return RunRecord{}, err
	}
	start, err := processIdentity(pid)
	if err != nil {
		return RunRecord{}, errors.New("cannot bind VM to a live owned process")
	}
	now := time.Now().UTC()
	rec := RunRecord{
		SchemaVersion: runSchemaVersion,
		ID:            id,
		OwnerUID:      os.Getuid(),
		PID:           pid,
		ProcessStart:  start,
		CreatedAt:     now,
		ExpiresAt:     now.Add(ttl),
	}
	file, err := os.OpenFile(filepath.Join(dir, "run.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return RunRecord{}, errors.New("VM run record already exists or cannot be created")
	}
	encodeErr := json.NewEncoder(file).Encode(rec)
	closeErr := file.Close()
	if encodeErr != nil || closeErr != nil {
		return RunRecord{}, errors.New("failed to commit VM run record")
	}
	return rec, nil
}

func (s *RunStore) saveSessionOutcome(outcome SessionOutcome) error {
	if !validRunID(outcome.ID) {
		return errors.New("invalid run ID for outcome")
	}
	path := filepath.Join(s.root, outcome.ID, "session.json")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create session outcome")
	}
	encodeErr := json.NewEncoder(file).Encode(outcome)
	closeErr := file.Close()
	if encodeErr != nil || closeErr != nil {
		return errors.New("cannot write session outcome")
	}
	return nil
}

// RunPinnedVMSession runs only a store-pinned trusted VM launcher. It is a
// FOREGROUND, operator-owned, single session—not a crash-safe daemon or
// agent-facing mutation API. Callers must use an unprivileged isolated worker.
// The generated NixOS script execs QEMU, keeping cmd.Process bound to QEMU.
func RunPinnedVMSession(ctx context.Context, store *RunStore, cgroups *CgroupManager, launcher string, ttl time.Duration, onStarted func(RunRecord)) (SessionOutcome, error) {
	if cgroups == nil {
		return SessionOutcome{}, errors.New("delegated cgroup resource limits are required")
	}
	if os.Geteuid() == 0 {
		return SessionOutcome{}, errors.New("refusing to launch VM supervisor as root")
	}
	if err := validatePinnedVM(launcher); err != nil {
		return SessionOutcome{}, err
	}
	kvm, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return SessionOutcome{}, errors.New("unprivileged KVM access is required")
	}
	_ = kvm.Close()
	return runVMSessionWithCgroup(ctx, store, cgroups, launcher, ttl, onStarted)
}

// runVMSession is separate for deterministic tests with a fake launcher.
// In production it is only called after pinned-store + KVM checks above.
func runVMSession(ctx context.Context, store *RunStore, launcher string, ttl time.Duration, onStarted func(RunRecord)) (SessionOutcome, error) {
	// Only the hostless fake-launcher tests use this unbounded helper.
	return runVMSessionWithCgroup(ctx, store, nil, launcher, ttl, onStarted)
}

func runVMSessionWithCgroup(ctx context.Context, store *RunStore, cgroups *CgroupManager, launcher string, ttl time.Duration, onStarted func(RunRecord)) (SessionOutcome, error) {
	if store == nil || ttl <= 0 || ttl > maxRunLifetime {
		return SessionOutcome{}, errors.New("invalid trusted VM supervisor configuration")
	}
	if err := ctx.Err(); err != nil {
		return SessionOutcome{}, err
	}
	id, dir, err := store.reserveVMRunWithTTL(ttl)
	if err != nil {
		return SessionOutcome{}, err
	}
	var cg *RunCgroup
	if cgroups != nil {
		cg, err = cgroups.Prepare(id)
		if err != nil {
			return SessionOutcome{ID: id}, err
		}
		defer cg.Close()
	}
	socket := filepath.Join(dir, guestAgentSocketName)
	cmd := exec.Command(launcher, vmLaunchArgs(socket)...)
	cmd.Dir = dir
	cmd.Env = vmLaunchEnvironment(dir)
	// Never copy guest console output or provider-controlled text to the CLI.
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cg != nil {
		if err := cg.Attach(cmd); err != nil {
			return SessionOutcome{ID: id}, err
		}
	}
	if err := cmd.Start(); err != nil {
		// The cgroup may have been prepared but no VM process launched.
		// Retain it if cleanup verification fails for any reason.
		if cg != nil {
			if cg.KillAndVerify() == nil {
				_ = cg.Remove()
			}
		}
		return SessionOutcome{ID: id}, errors.New("pinned VM process could not start; inspect private run directory")
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	rec, err := store.bindVMRun(id, cmd.Process.Pid, ttl)
	if err != nil {
		_ = cmd.Process.Kill()
		<-waited
		if cg != nil {
			if cg.KillAndVerify() == nil {
				_ = cg.Remove()
			}
		}
		return SessionOutcome{ID: id}, err
	}
	if onStarted != nil {
		// Process registration does not imply guest boot or QGA readiness.
		onStarted(rec)
	}
	timer := time.NewTimer(ttl)
	defer timer.Stop()

	var runErr error
	reason := "exited"
	select {
	case runErr = <-waited:
	case <-ctx.Done():
		reason = "cancelled"
		runErr = ctx.Err()
	case <-timer.C:
		reason = "expired"
		runErr = ErrSessionExpired
	}
	if reason != "exited" {
		// Only the process HANDLE returned by os/exec is signalled; no
		// unchecked raw PID, group kill or foreign process-tree traversal.
		_ = cmd.Process.Signal(syscall.SIGTERM)
		grace := time.NewTimer(sessionShutdownGrace)
		select {
		case <-waited:
		case <-grace.C:
			_ = cmd.Process.Kill()
			<-waited
		}
		grace.Stop()
	}
	cgroupState := "not_verified"
	var cgroupErr error
	if cg != nil {
		if err := cg.KillAndVerify(); err != nil {
			cgroupErr = fmt.Errorf("run cgroup teardown unverified: %w", err)
		} else if err := cg.Remove(); err != nil {
			cgroupErr = fmt.Errorf("run cgroup could not be removed: %w", err)
		} else {
			cgroupState = "empty_and_removed"
		}
	}
	outcome := SessionOutcome{
		ID:            id,
		EndedAt:       time.Now().UTC(),
		Reason:        reason,
		ProcessReaped: true,
		Cgroup:        cgroupState,
		Cleanup:       "not_verified",
		Upstream:      "not_verified",
	}
	if err := store.saveSessionOutcome(outcome); err != nil {
		return outcome, errors.Join(err, cgroupErr)
	}
	if reason == "exited" && runErr != nil {
		return outcome, errors.Join(errors.New("pinned VM process exited unsuccessfully"), cgroupErr)
	}
	return outcome, errors.Join(runErr, cgroupErr)
}
