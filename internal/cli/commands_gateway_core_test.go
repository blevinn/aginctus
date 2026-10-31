package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCobraGatewayCoreRejectsInvalidSyntaxBeforeConfig(t *testing.T) {
	for _, args := range [][]string{
		{"gateway", "validate", "unexpected"},
		{"gateway", "status", "--force"},
		{"gateway", "credential-storage", "audit", "--force"},
		{"gateway", "credential-storage", "attach", "other"},
		{"gateway", "apply", "--force"},
		{"gateway", "remove", "extra"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var out, errors bytes.Buffer
			code := Run(context.Background(), args, &out, &errors, nil, &fakeConfigLoader{err: context.Canceled})
			if code != 2 || out.Len() != 0 || !strings.Contains(errors.String(), "Usage:") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errors.String())
			}
			if strings.Contains(errors.String(), "configuration:") {
				t.Fatalf("configuration read on bad gateway syntax: %q", errors.String())
			}
		})
	}
}
