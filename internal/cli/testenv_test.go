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
		{"testenv", "mcp"},
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
	executeTestenvVM = func(context.Context, string, string, io.Writer, io.Writer) error {
		return errors.New("guest VM failed")
	}
	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{"testenv", "vm", "test", "smoke"}, &out, &errOut, nil, nil); code != 1 || !strings.Contains(errOut.String(), "guest VM failed") {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
}
