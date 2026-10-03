package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeIncusClient struct {
	version    string
	versionErr error
	daemonErr  error
}

func (c fakeIncusClient) Version(context.Context) (string, error) {
	return c.version, c.versionErr
}

func (c fakeIncusClient) CheckDaemon(context.Context) error {
	return c.daemonErr
}

func TestDoctorSuccess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"doctor"}, &stdout, &stderr, fakeIncusClient{version: "6.0.0"})

	if code != 0 {
		t.Fatalf("Run() code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "incus client: 6.0.0") || !strings.Contains(got, "incus daemon: reachable") {
		t.Fatalf("stdout = %q", got)
	}
}

func TestDoctorDaemonFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"doctor"}, &stdout, &stderr, fakeIncusClient{
		version:   "6.0.0",
		daemonErr: errors.New("connection refused"),
	})

	if code != 1 {
		t.Fatalf("Run() code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "incus daemon: unreachable") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"nope"}, &stdout, &stderr, fakeIncusClient{})

	if code != 2 {
		t.Fatalf("Run() code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
