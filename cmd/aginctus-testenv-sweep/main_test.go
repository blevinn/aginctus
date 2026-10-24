package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/blevinn/aginctus/internal/testenv"
)

func TestSweepExecutableRejectsArgumentsBeforeAnyPrivileges(t *testing.T) {
	original := sweepExpired
	defer func() { sweepExpired = original }()
	called := false
	sweepExpired = func(context.Context, testenv.TrustedIncusSweepOptions, time.Time) (testenv.IncusReconcileResult, error) {
		called = true
		return testenv.IncusReconcileResult{}, nil
	}
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"--socket=/tmp/other"}, func(string) string {return ""}, &out, &errOut)
	if code != 2 || called || out.Len() != 0 {
		t.Fatalf("arbitrary daemon argument reached worker: %d %t %q", code, called, out.String())
	}
}

func TestSweepExecutableOnlyReportsAggregateCounts(t *testing.T) {
	original := sweepExpired
	defer func() { sweepExpired = original }()
	sweepExpired = func(_ context.Context, cfg testenv.TrustedIncusSweepOptions, _ time.Time) (testenv.IncusReconcileResult, error) {
		if cfg.SocketPath != "/var/lib/incus/unix.socket" ||
			cfg.StoragePool != "testenv" || cfg.PolicyKeyPath != "/run/credentials/key" {
			t.Fatalf("unexpected configured worker paths: %#v", cfg)
		}
		return testenv.IncusReconcileResult{Scanned: 2, Skipped: 1, Destroyed: 1}, nil
	}
	env := map[string]string{
		"AGINCTUS_TESTENV_WORKER_SOCKET": "/var/lib/incus/unix.socket",
		"AGINCTUS_TESTENV_WORKER_POOL": "testenv",
		"AGINCTUS_TESTENV_POLICY_KEY_FILE": "/run/credentials/key",
	}
	var out, errOut bytes.Buffer
	code := run(context.Background(), nil, func(k string) string { return env[k] }, &out, &errOut)
	if code != 0 || errOut.Len() != 0 || !strings.Contains(out.String(), "scanned=2 skipped=1 destroyed=1 failed=0") {
		t.Fatalf("wrong bounded summary: %d %q %q", code, out.String(), errOut.String())
	}
}

func TestSweepExecutableRedactsUntrustedFailures(t *testing.T) {
	original := sweepExpired
	defer func() { sweepExpired = original }()
	sweepExpired = func(context.Context, testenv.TrustedIncusSweepOptions, time.Time) (testenv.IncusReconcileResult, error) {
		return testenv.IncusReconcileResult{Scanned: 1, Failed: 1},
			errors.New("provider leaked token=secret and run-path=/pilot/data")
	}
	var out, errOut bytes.Buffer
	code := run(context.Background(), nil, func(string) string {return ""}, &out, &errOut)
	if code != 1 || !strings.Contains(out.String(), "failed=1") ||
		strings.Contains(out.String()+errOut.String(), "secret") ||
		strings.Contains(out.String()+errOut.String(), "/pilot") {
		t.Fatalf("untrusted failure leaked or marked pass: %d %q %q", code, out.String(), errOut.String())
	}
}
