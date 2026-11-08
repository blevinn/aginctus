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

func TestWorkerLeavesAreInaccessibleFromHelpAndInvalidArguments(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"worker", "preflight", "--help"}, 0},
		{[]string{"worker", "preflight", "--exec", "sh"}, 2},
		{[]string{"worker", "preflight", "--pool-fingerprint", "--exec"}, 2},
		{[]string{"worker", "sweep", "--socket=/tmp/other"}, 2},
		{[]string{"worker", "acceptance", "--command", "/bin/sh"}, 2},
		{[]string{"worker", "acceptance", "exec"}, 2},
		{[]string{"worker", "acceptance", "--help"}, 0},
	} {
		services := workerServices{
			getenv: func(string) string { t.Fatal("read privileged environment"); return "" },
			preflight: func(context.Context, testenv.TrustedIncusPreflightOptions) (testenv.IncusWorkerPreflightReport, error) {
				t.Fatal("contacted preflight backend"); return testenv.IncusWorkerPreflightReport{}, nil
			},
			accept: func(context.Context, testenv.IncusAcceptanceOptions) (testenv.IncusAcceptanceReport, error) {
				t.Fatal("contacted acceptance backend"); return testenv.IncusAcceptanceReport{}, nil
			},
			sweep: func(context.Context, testenv.TrustedIncusSweepOptions, time.Time) (testenv.IncusReconcileResult, error) {
				t.Fatal("contacted recovery backend"); return testenv.IncusReconcileResult{}, nil
			},
			now: time.Now,
		}
		var out, stderr bytes.Buffer
		if code := executeWithServices(context.Background(), tc.args, strings.NewReader(""), &out, &stderr, services); code != tc.code {
			t.Fatalf("wrong code for %v: %d %s", tc.args, code, stderr.String())
		}
	}
}

func TestWorkerPreflightRedactsErrorAndPreservesInspection(t *testing.T) {
	for _, inspect := range []bool{false, true} {
		services := defaultWorkerServices()
		services.getenv = func(k string) string { return k }
		services.preflight = func(_ context.Context, cfg testenv.TrustedIncusPreflightOptions) (testenv.IncusWorkerPreflightReport, error) {
			if cfg.InspectPool != inspect || cfg.SocketPath != "AGINCTUS_TESTENV_WORKER_SOCKET" ||
				cfg.PolicyKeyPath != "AGINCTUS_TESTENV_POLICY_KEY_FILE" {
				t.Fatal("incorrect worker configuration", cfg)
			}
			return testenv.IncusWorkerPreflightReport{}, errors.New("private-credential-bearing-daemon-diagnostic")
		}
		args := []string{"worker", "preflight"}
		if inspect { args = append(args, "--pool-fingerprint") }
		var out, stderr bytes.Buffer
		code := executeWithServices(context.Background(), args, strings.NewReader(""), &out, &stderr, services)
		if code != 1 || !strings.Contains(out.String(), `"status":"fail"`) ||
			strings.Contains(out.String()+stderr.String(), "private-credential") {
			t.Fatal(code, out.String(), stderr.String())
		}
	}
}

func TestWorkerSweepCountsAndFailureRedaction(t *testing.T) {
	services := defaultWorkerServices()
	env := map[string]string{
		"AGINCTUS_TESTENV_WORKER_SOCKET": "/var/lib/incus/unix.socket",
		"AGINCTUS_TESTENV_WORKER_POOL": "testenv",
		"AGINCTUS_TESTENV_POLICY_KEY_FILE": "/run/credentials/key",
	}
	services.getenv = func(k string) string { return env[k] }
	services.sweep = func(_ context.Context, cfg testenv.TrustedIncusSweepOptions, _ time.Time) (testenv.IncusReconcileResult, error) {
		if cfg.SocketPath != env["AGINCTUS_TESTENV_WORKER_SOCKET"] ||
			cfg.StoragePool != "testenv" || cfg.PolicyKeyPath != env["AGINCTUS_TESTENV_POLICY_KEY_FILE"] {
			t.Fatal("incorrect installed options", cfg)
		}
		return testenv.IncusReconcileResult{Scanned: 2, Skipped: 1, Destroyed: 1}, nil
	}
	var out, stderr bytes.Buffer
	args := []string{"worker", "sweep"}
	if code := executeWithServices(context.Background(), args, strings.NewReader(""), &out, &stderr, services); code != 0 ||
		!strings.Contains(out.String(), "scanned=2 skipped=1 destroyed=1 failed=0") {
		t.Fatal(code, out.String(), stderr.String())
	}
	services.sweep = func(context.Context, testenv.TrustedIncusSweepOptions, time.Time) (testenv.IncusReconcileResult, error) {
		return testenv.IncusReconcileResult{Scanned: 1, Failed: 1}, errors.New("token=secret /pilot/path")
	}
	out.Reset(); stderr.Reset()
	if code := executeWithServices(context.Background(), args, strings.NewReader(""), &out, &stderr, services); code != 1 ||
		!strings.Contains(out.String(), "failed=1") || strings.Contains(out.String()+stderr.String(), "secret") ||
		strings.Contains(out.String()+stderr.String(), "/pilot/path") {
		t.Fatal(code, out.String(), stderr.String())
	}
}

func TestWorkerAcceptancePreservesSummaryAndRevision(t *testing.T) {
	services := defaultWorkerServices()
	services.accept = func(_ context.Context, cfg testenv.IncusAcceptanceOptions) (testenv.IncusAcceptanceReport, error) {
		if cfg.Revision != revision || cfg.ImageFingerprint != "operator-image" || cfg.SocketPath != "/operator/socket" {
			t.Fatal("incorrect worker options", cfg)
		}
		return testenv.IncusAcceptanceReport{
			RunID: strings.Repeat("a", 24),
			Handles: []testenv.VMRef{{PolicyMAC: "private-handle"}},
		}, errors.New("private-credential-secret")
	}
	var out, stderr bytes.Buffer
	code := executeWithServices(context.Background(), []string{"worker", "acceptance",
		"--image", "operator-image", "--socket", "/operator/socket"}, strings.NewReader(""), &out, &stderr, services)
	if code != 1 || !strings.Contains(out.String(), `"status":"fail"`) ||
		!strings.Contains(out.String(), `"profile":"dedicated_worker"`) {
		t.Fatal(code, out.String(), stderr.String())
	}
	for _, private := range []string{"private-credential-secret", "private-handle", "operator-image", "/operator/socket"} {
		if strings.Contains(out.String()+stderr.String(), private) {
			t.Fatalf("private value leaked: %s", private)
		}
	}
}
