package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestHelpExamplesUsesReadOnlyAuthoredExamples(t *testing.T) {
	for _, tc := range []struct{
		args []string
		want string
	}{
		{[]string{"help", "--examples", "environment"}, "aginctus environment list"},
		{[]string{"help", "--examples", "environment", "list"}, "--incus-project lab"},
		{[]string{"help", "--examples", "environment", "show"}, "<environment-uuid>"},
		{[]string{"help", "--full", "environment"}, "Subcommand syntax"},
	} {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), tc.args, &stdout, &stderr, nil, nil)
		if code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), tc.want) {
			t.Fatalf("%q: code=%d stdout=%q stderr=%q", tc.args, code, stdout.String(), stderr.String())
		}
	}
}

func TestHelpExamplesRejectsUnknownModes(t *testing.T) {
	for _, args := range [][]string{
		{"help", "--bogus", "environment"},
		{"help", "--full", "--examples", "environment"},
		{"help", "environment", "--examples"},
	} {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), args, &stdout, &stderr, nil, nil)
		if code != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("%q: code=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}
}
