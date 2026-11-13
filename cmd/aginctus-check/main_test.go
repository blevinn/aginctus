package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRejectsUnsafeOrUnknownFlagsBeforeAnySuite(t *testing.T) {
	for _, args := range [][]string{
		{"--format", "json", "--verbose"},
		{"--format", "xml"},
		{"--vm-run-id", "invalid", "--verbose"},
		{"--vm-run-id", "", "--verbose"},
		{"--vm-run-id"},
		{"--unknown"},
		{"extra"},
	} {
		var out, err bytes.Buffer
		if code := execute(context.Background(), args, &out, &err); code != 2 {
			t.Fatalf("unexpected argument status for %q: %d", args, code)
		}
		if out.Len() != 0 {
			t.Fatalf("invalid arguments produced report for %q", args)
		}
	}
}

func TestExplicitEmptyVMRunIDFailsClosed(t *testing.T) {
	working, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(filepath.Clean(filepath.Join(working, "..", ".."))); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(working); err != nil {
			t.Fatal(err)
		}
	}()
	var out, stderr bytes.Buffer
	if code := execute(context.Background(), []string{"--format=json", "--vm-run-id="}, &out, &stderr); code != 1 {
		t.Fatalf("empty VM run ID returned %d: %s", code, stderr.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"status":"error"`)) {
		t.Fatalf("empty VM selector did not emit an error report: %s", out.String())
	}
}

func TestCoverageAndAcceptanceSelectionCannotRunDefaultSuites(t *testing.T) {
	working, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(filepath.Clean(filepath.Join(working, "..", ".."))); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(working); err != nil {
			t.Fatal(err)
		}
	}()
	for _, args := range [][]string{
		{"--format=json", "--coverage"},
		{"--format=json", "--acceptance-run-id=invalid"},
	} {
		var stdout, stderr bytes.Buffer
		if code := execute(context.Background(), args, &stdout, &stderr); code != 1 {
			t.Fatalf("read-only selector returned %d: %s", code, stderr.String())
		}
		if !bytes.Contains(stdout.Bytes(), []byte("\"schema_version\":1")) ||
			!bytes.Contains(stdout.Bytes(), []byte("\"complete_acceptance\":false")) &&
				!bytes.Contains(stdout.Bytes(), []byte("\"profile\":\"local_acceptance_view\"")) {
			t.Fatalf("invalid read-only reporting: %s", stdout.String())
		}
	}
	for _, args := range [][]string{
		{"--hermetic-json=/tmp/fixture.json"},
		{"--coverage", "--hermetic-json="},
		{"--coverage", "--vm-run-id=invalid"},
		{"--acceptance-run-id=foo", "--vm-run-id=bar"},
		{"--coverage", "--verbose"},
	} {
		var stdout, stderr bytes.Buffer
		if code := execute(context.Background(), args, &stdout, &stderr); code != 2 || stdout.Len() != 0 {
			t.Fatalf("conflicting selector did not fail before execution: %q, %d, %s", args, code, stdout.String())
		}
	}
}
