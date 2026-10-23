package testenv

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sessionTestStore(t *testing.T) *RunStore {
	t.Helper()
	root := filepath.Join(t.TempDir(), "runs")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRunStore(root)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func sessionTestLauncher(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-vm-launcher")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPinnedVMLauncherRejectsMutablePaths(t *testing.T) {
	for _, path := range []string{"", "relative", "/tmp/agent/runner", "/nix/store/../../tmp/runner"} {
		if err := validatePinnedVM(path); err == nil {
			t.Fatalf("accepted non-store launch path %q", path)
		}
	}
}

func TestFixedQEMUArgumentsAndSanitizedEnvironment(t *testing.T) {
	args := vmLaunchArgs("/private/vm-123/qga.sock")
	got := strings.Join(args, "|")
	for _, expected := range []string{
		"socket,id=aginctus-qga,path=/private/vm-123/qga.sock,server=on,wait=off",
		"virtio-serial-pci",
		"virtserialport,chardev=aginctus-qga,name=org.qemu.guest_agent.0",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("missing pinned QGA option %q: %#v", expected, args)
		}
	}
	os.Setenv("QEMU_OPTS", "-netdev user,id=escape")
	os.Setenv("SSH_AUTH_SOCK", "/tmp/agent.sock")
	t.Cleanup(func() { os.Unsetenv("QEMU_OPTS"); os.Unsetenv("SSH_AUTH_SOCK") })
	env := strings.Join(vmLaunchEnvironment("/private/run"), "|")
	for _, disallowed := range []string{"QEMU_OPTS=", "SSH_AUTH_SOCK=", "INCUS_", "NIX_PATH=", "OPENAI_API_KEY="} {
		if strings.Contains(env, disallowed) {
			t.Fatalf("inherited unwanted env %q: %q", disallowed, env)
		}
	}
	for _, want := range []string{"NIX_DISK_IMAGE=/private/run/root.qcow2", "TMPDIR=/private/run/tmp", "USE_TMPDIR=1"} {
		if !strings.Contains(env, want) {
			t.Errorf("missing %q in %q", want, env)
		}
	}
}

func TestSupervisedVMRunCancelReapsProcessAndRetainsEvidence(t *testing.T) {
	store := sessionTestStore(t)
	launcher := sessionTestLauncher(t, "printf '%s\\n' \"$@\" > \"$TMPDIR/argv\"; printf '%s\\n' \"$QEMU_OPTS\" > \"$TMPDIR/opts\"; exec sleep 30")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct { outcome SessionOutcome; err error }
	done := make(chan result, 1)
	ready := make(chan RunRecord, 1)
	go func() {
		out, err := runVMSession(ctx, store, launcher, time.Minute, func(rec RunRecord) { ready <- rec })
		done <- result{out, err}
	}()
	var rec RunRecord
	select {
	case rec = <-ready:
	case <-time.After(5*time.Second):
		t.Fatal("VM process never registered")
	}
	status, err := store.Status(rec.ID)
	if err != nil || !status.Running {
		t.Fatalf("not alive after start: %#v %v", status, err)
	}
	if rec.OwnerUID != os.Getuid() || rec.ProcessStart == "" {
		t.Fatalf("unbound process: %#v", rec)
	}
	// Ensure the fake script received the exact QGA flags, without an
	// agent-defined network/device or a forwarded host environment.
	argvFile := filepath.Join(store.root, rec.ID, "tmp", "argv")
	deadline := time.Now().Add(3*time.Second)
	for {
		if _, err := os.Stat(argvFile); err == nil { break }
		if time.Now().After(deadline) { t.Fatal("fake launcher never entered") }
		time.Sleep(10*time.Millisecond)
	}
	args, err := os.ReadFile(argvFile)
	if err != nil { t.Fatal(err) }
	if !strings.Contains(string(args), "org.qemu.guest_agent.0") ||
		strings.Contains(string(args), "-nic") || strings.Contains(string(args), "/etc/shadow") {
		t.Fatalf("unsafe launcher arguments: %q", args)
	}
	cancel()
	select {
	case r := <-done:
		if !errors.Is(r.err, context.Canceled) || r.outcome.ID != rec.ID ||
			r.outcome.Reason != "cancelled" || !r.outcome.ProcessReaped ||
			r.outcome.Cleanup != "not_verified" || r.outcome.Upstream != "not_verified" {
			t.Fatalf("unexpected run result: %#v, %v", r.outcome, r.err)
		}
	case <-time.After(6*time.Second):
		t.Fatal("supervisor did not reap process after cancellation")
	}
	status, err = store.Status(rec.ID)
	if err != nil || status.Running {
		t.Fatalf("process not gone: %#v %v", status, err)
	}
	report, err := os.ReadFile(filepath.Join(store.root, rec.ID, "session.json"))
	if err != nil { t.Fatal(err) }
	var outcome SessionOutcome
	if err := json.Unmarshal(report, &outcome); err != nil || !outcome.ProcessReaped {
		t.Fatalf("missing bounded session evidence: %s, %v", report, err)
	}
	if _, err := os.Stat(filepath.Join(store.root, rec.ID, "run.json")); err != nil {
		t.Fatal("run record unexpectedly deleted")
	}
}

func TestSupervisedVMRunExpiresWithoutExternalCancel(t *testing.T) {
	store := sessionTestStore(t)
	launcher := sessionTestLauncher(t, "exec sleep 30")
	out, err := runVMSession(context.Background(), store, launcher, 250*time.Millisecond, nil)
	if !errors.Is(err, ErrSessionExpired) || out.Reason != "expired" || !out.ProcessReaped {
		t.Fatalf("expiration did not stop process: %#v, %v", out, err)
	}
	status, err := store.Status(out.ID)
	if err != nil || status.Running {
		t.Fatalf("expired process remains running: %#v, %v", status, err)
	}
}

func TestRunSessionDoesNotDeleteUnrelatedArtifacts(t *testing.T) {
	store := sessionTestStore(t)
	foreign := filepath.Join(store.root, "do-not-remove")
	if err := os.WriteFile(foreign, []byte("foreign-data"), 0600); err != nil {
		t.Fatal(err)
	}
	launcher := sessionTestLauncher(t, "exec sleep 30")
	out, err := runVMSession(context.Background(), store, launcher, 100*time.Millisecond, nil)
	if !errors.Is(err, ErrSessionExpired) || !validRunID(out.ID) {
		t.Fatalf("unexpected run: %#v %v", out, err)
	}
	b, err := os.ReadFile(foreign)
	if err != nil || string(b) != "foreign-data" {
		t.Fatalf("supervisor damaged unrelated state: %q %v", b, err)
	}
}

func TestSupervisorRejectsUnsafeLifetimeAndCancelledStartup(t *testing.T) {
	store := sessionTestStore(t)
	launcher := sessionTestLauncher(t, "exec sleep 1")
	for _, ttl := range []time.Duration{0, -time.Second, maxRunLifetime+time.Second} {
		if _, err := runVMSession(context.Background(), store, launcher, ttl, nil); err == nil {
			t.Fatalf("accepted TTL %s", ttl)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runVMSession(ctx, store, launcher, time.Second, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("started a VM after cancellation: %v", err)
	}
}
