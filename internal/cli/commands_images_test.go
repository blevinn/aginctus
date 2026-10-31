package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCobraImagesRequireExplicitAuthorityBeforeConfig(t *testing.T) {
	for _, args := range [][]string{
		{"images", "import", "--project=aginctus", "x", "/tmp"},
		{"images", "import-owned", "--project=aginctus", "--owner=user", "x", "/tmp"},
		{"images", "owner-audit", "--project=aginctus", "x"},
		{"images", "replace-plan", "--project=aginctus", "--owner=bad", "x", "/tmp"},
		{"images", "list", "--confirm-create"},
		{"images", "update", "x"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var out, errors bytes.Buffer
			code := Run(context.Background(), args, &out, &errors, nil, &fakeConfigLoader{err: context.Canceled})
			if code != 2 || out.Len() != 0 || !strings.Contains(errors.String(), "Usage:") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errors.String())
			}
			if strings.Contains(errors.String(), "configuration:") {
				t.Fatalf("loaded config on syntax error: %q", errors.String())
			}
		})
	}
}
