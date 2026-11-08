package main

import (
	"bytes"
	"context"
	"testing"
)

func TestRejectsUnsafeOrUnknownFlagsBeforeAnySuite(t *testing.T) {
	for _, args := range [][]string{
		{"--format", "json", "--verbose"},
		{"--format", "xml"},
		{"--vm-run-id", "invalid", "--verbose"},
		{"--vm-run-id", "", "--verbose"},
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
