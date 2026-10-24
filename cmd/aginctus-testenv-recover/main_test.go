package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestConfiguredRootsRejectsArgumentsAndMissingPolicy(t *testing.T) {
	env := func(key string) string {
		switch key {
		case "AGINCTUS_TESTENV_RUN_ROOT":
			return "/var/lib/aginctus-testenv/runs"
		case "AGINCTUS_TESTENV_CGROUP_ROOT":
			return "/sys/fs/cgroup/system.slice/aginctus-testenv-keeper.service"
		}
		return ""
	}
	for _, args := range [][]string{
		{"--state-root=/tmp/agent"},
		{"exec", "/bin/sh"},
		{"--help"},
	} {
		if _, _, err := configuredRoots(args, env); err == nil {
			t.Fatalf("accepted untrusted arguments %v", args)
		}
	}
	if _, _, err := configuredRoots(nil, func(string) string { return "" }); err == nil {
		t.Fatal("accepted unconfigured policy")
	}
	state, cg, err := configuredRoots(nil, env)
	if err != nil || state != "/var/lib/aginctus-testenv/runs" ||
		cg != "/sys/fs/cgroup/system.slice/aginctus-testenv-keeper.service" {
		t.Fatalf("unexpected policy selection: %q %q %v", state, cg, err)
	}
}

func TestRecoveryNoPolicyFailsBeforeAccessingHost(t *testing.T) {
	err := run(context.Background(), nil, func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("unconfigured watchdog unexpectedly ran: %v", err)
	}
}

func TestRecoveryRejectsCancellationAndUntrustedMode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	env := func(key string) string {
		if key == "AGINCTUS_TESTENV_RUN_ROOT" {
			return "/nonexistent"
		}
		return "/nonexistent"
	}
	err := run(ctx, nil, env)
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		// Missing worker privileges/store must not make a successful run.
		t.Fatalf("watchdog improperly passed: %v", err)
	}
}
