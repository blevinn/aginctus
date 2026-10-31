package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/testenv"
)

func TestAcceptanceEntrypointRejectsCommandOverrides(t *testing.T) {
	for _, args := range [][]string{{"--command", "/bin/sh"}, {"--root", "/tmp"}, {"exec"}, {"--help"}} {
		called := false
		var out, stderr bytes.Buffer
		run(context.Background(), args, &out, &stderr, func(context.Context, testenv.IncusAcceptanceOptions) (testenv.IncusAcceptanceReport, error) {
			called = true
			return testenv.IncusAcceptanceReport{}, nil
		})
		if called {
			t.Fatalf("invalid invocation reached worker: %v", args)
		}
	}
}

func TestAcceptanceEntrypointRedactsErrorsAndHandles(t *testing.T) {
	var out, stderr bytes.Buffer
	code := run(context.Background(), []string{"--image", "operator-image", "--socket", "/operator/socket"}, &out, &stderr,
		func(_ context.Context, cfg testenv.IncusAcceptanceOptions) (testenv.IncusAcceptanceReport, error) {
			if cfg.ImageFingerprint != "operator-image" || cfg.SocketPath != "/operator/socket" || cfg.Revision != revision {
				t.Fatal(cfg)
			}
			return testenv.IncusAcceptanceReport{RunID: strings.Repeat("a", 24), Handles: []testenv.VMRef{{PolicyMAC: "private-handle"}}}, errors.New("synthetic-secret")
		})
	if code != 1 || !strings.Contains(out.String(), `"status":"fail"`) {
		t.Fatalf("incorrect failure: %d %s", code, &out)
	}
	for _, secret := range []string{"synthetic-secret", "private-handle", "operator-image", "/operator/socket"} {
		if strings.Contains(out.String()+stderr.String(), secret) {
			t.Fatal("private diagnostic leaked")
		}
	}
}
