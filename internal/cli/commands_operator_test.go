package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCobraOperatorInvalidInputDoesNotReadConfigOrInspectIncus(t *testing.T) {
	for _, args := range [][]string{
		{"operator", "preflight"},
		{"operator", "project-ensure", "--project=aginctus"},
		{"operator", "project-plan"},
		{"operator", "units-audit", "--owner=system"},
		{"operator", "units-audit", "--owner=bad"},
		{"operator", "package-audit", "--owner=user"},
		{"operator", "state-audit", "--owner=user"},
		{"operator", "state-audit", "--location=bad"},
		{"operator", "state-audit"},
		{"--config=incus.project=other", "operator", "project-plan", "--project=aginctus"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var out, errs bytes.Buffer
			code := Run(context.Background(), args, &out, &errs, nil, &fakeConfigLoader{err: context.Canceled})
			if code != 2 || out.Len() != 0 || !strings.Contains(errs.String(), "Usage:") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errs.String())
			}
			if strings.Contains(errs.String(), "configuration:") {
				t.Fatalf("invalid syntax loaded config: %q", errs.String())
			}
		})
	}
}
