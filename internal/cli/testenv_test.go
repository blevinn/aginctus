package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestTestenvOfflineHelpListAndPlan(t *testing.T) {
	for _, args := range [][]string{
		{"testenv", "--help"}, {"help", "testenv"},
		{"testenv", "vm", "list"},
		{"testenv", "vm", "backend"},
		{"testenv", "vm", "backend", "incus"},
		{"testenv", "vm", "policy"},
		{"testenv", "vm", "plan", "smoke"},
		{"--configuration-file=/does/not/exist", "testenv", "--help"},
	} {
		var out, errOut bytes.Buffer
		// Nil config loader and Incus prove the operation is hostless.
		if code := Run(context.Background(), args, &out, &errOut, nil, nil); code != 0 || out.Len() == 0 || errOut.Len() != 0 {
			t.Fatalf("%v: code=%d stdout=%q stderr=%q", args, code, out.String(), errOut.String())
		}
	}
}

func TestTestenvRejectsUnexpectedCommandsWithoutExecution(t *testing.T) {
	before := executeTestenvVM
	defer func() { executeTestenvVM = before }()
	called := false
	executeTestenvVM = func(context.Context, string, string, io.Writer, io.Writer) error {
		called = true
		return nil
	}
	for _, args := range [][]string{
		{"testenv"},
		{"testenv", "vm", "test"},
		{"testenv", "vm", "test", "../smoke"},
		{"testenv", "vm", "test", "smoke", "--nix-option"},
		{"testenv", "vm", "exec", "/bin/sh"},
		{"testenv", "vm", "backend", "qemu"},
		{"testenv", "vm", "policy", "--pool=pilot"},
		{"testenv", "vm", "backend", "incus", "--host-socket=/var/lib/incus/unix.socket"},
		{"testenv", "mcp", "--http"},
		{"testenv", "vm", "plan", ".#vm-harness-smoke"},
		{"--config=workloads.dev.isolation=vm", "testenv", "vm", "test", "smoke"},
	} {
		called = false
		var out, errOut bytes.Buffer
		code := Run(context.Background(), args, &out, &errOut, nil, nil)
		if code != 2 || called || out.Len() != 0 {
			t.Fatalf("%v: code=%d called=%t stdout=%q stderr=%q", args, code, called, out.String(), errOut.String())
		}
	}
}

func TestTestenvDispatchesOnlyKnownScenario(t *testing.T) {
	before := executeTestenvVM
	defer func() { executeTestenvVM = before }()
	var received string
	executeTestenvVM = func(_ context.Context, root, scenario string, stdout, _ io.Writer) error {
		if root == "" {
			t.Fatal("missing root")
		}
		received = scenario
		_, _ = io.WriteString(stdout, "simulated batch PASS\n")
		return nil
	}
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"testenv", "vm", "test", "smoke"}, &out, &errOut, nil, nil); code != 0 || received != "smoke" || !strings.Contains(out.String(), "PASS") {
		t.Fatalf("code=%d scenario=%q stderr=%q", code, received, errOut.String())
	}
	executeTestenvVM = func(context.Context, string, string, io.Writer, io.Writer) error { return errors.New("guest VM failed") }
	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{"testenv", "vm", "test", "smoke"}, &out, &errOut, nil, nil); code != 1 || !strings.Contains(errOut.String(), "guest VM failed") {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
}

func TestTestenvMCPIsConfigFreeAndReadOnly(t *testing.T) {
	before := serveTestenvMCP
	defer func() { serveTestenvMCP = before }()
	called := false
	serveTestenvMCP = func(_ context.Context, _ io.Reader, out io.Writer) error {
		called = true
		_, _ = io.WriteString(out, "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n")
		return nil
	}
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"testenv", "mcp"}, &out, &errOut, nil, nil); code != 0 || !called || !strings.Contains(out.String(), "jsonrpc") {
		t.Fatalf("code=%d called=%v stdout=%q stderr=%q", code, called, out.String(), errOut.String())
	}
}

func TestTestenvBackendDiscoveryIsReadOnlyAndUninstalled(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"testenv", "vm", "backend", "incus"}, &out, &errOut, nil, nil)
	if code != 0 || !strings.Contains(out.String(), "\"lifecycle\": \"planned\"") ||
		!strings.Contains(out.String(), "\"agent_accessible\": false") ||
		errOut.Len() != 0 {
		t.Fatalf("unexpected backend discovery: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}

func TestTestenvIncusPolicyIsOfflineAndRestricted(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"testenv", "vm", "policy"}, &out, &errOut, nil, nil)
	if code != 0 || errOut.Len() != 0 ||
		!strings.Contains(out.String(), "\"restricted\": \"true\"") ||
		!strings.Contains(out.String(), "\"restricted.devices.nic\": \"block\"") ||
		!strings.Contains(out.String(), "\"restricted.virtual-machines.nesting\": \"allow\"") ||
		!strings.Contains(out.String(), "\"limits.instances\": \"2\"") {
		t.Fatalf("invalid offline policy: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}
