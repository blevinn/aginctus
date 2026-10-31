package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCobraHerdrRejectsMalformedInputBeforeConfig(t *testing.T) {
	for _, args := range [][]string{
		{"herdr", "authorize"},
		{"herdr", "register", "a", "b"},
		{"herdr", "client", "apply", "--project=default"},
		{"herdr", "client", "status", "--force"},
		{"herdr", "client", "stop", "--dry-run"},
		{"herdr", "client", "remove", "extra"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), args, &stdout, &stderr, nil, &fakeConfigLoader{err: context.Canceled})
			if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "Usage:") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if strings.Contains(stderr.String(), "configuration:") {
				t.Fatalf("loaded configuration on invalid input: %q", stderr.String())
			}
		})
	}
}
