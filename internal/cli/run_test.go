package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeIncusClient struct {
	version        string
	versionErr     error
	networkCreated bool
	networkErr     error
}

func (c fakeIncusClient) ServerVersion(context.Context) (string, error) {
	return c.version, c.versionErr
}

func (c fakeIncusClient) EnsureManagementNetwork(context.Context) (bool, error) {
	return c.networkCreated, c.networkErr
}

func TestDoctorSuccess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"doctor"}, &stdout, &stderr, fakeIncusClient{version: "7.0.1"})

	if code != 0 {
		t.Fatalf("Run() code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "incus daemon: reachable (7.0.1)") {
		t.Fatalf("stdout = %q", got)
	}
}

func TestNetworkEnsureCreated(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"network", "ensure"}, &stdout, &stderr, fakeIncusClient{networkCreated: true})

	if code != 0 {
		t.Fatalf("Run() code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "management network: created") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestNetworkEnsureFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"network", "ensure"}, &stdout, &stderr, fakeIncusClient{
		networkErr: errors.New("ownership conflict"),
	})

	if code != 1 {
		t.Fatalf("Run() code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "ownership conflict") {
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
