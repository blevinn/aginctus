package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCobraTestenvSyntaxDoesNotCallConfig(t *testing.T) {
	for _, args := range [][]string{
		{"testenv", "vm", "test"},
		{"testenv", "vm", "policy", "oops"},
		{"testenv", "vm", "backend", "incus", "extra"},
		{"testenv", "mcp", "--config=foo"},
		{"--config=a.b=c", "testenv", "vm", "list"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var out, errors bytes.Buffer
			code := Run(context.Background(), args, &out, &errors, nil, &fakeConfigLoader{err: context.Canceled})
			if code != 2 || out.Len() != 0 || !strings.Contains(errors.String(), "Usage:") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errors.String())
			}
			if strings.Contains(errors.String(), "configuration:") {
				t.Fatalf("loaded config on syntax failure: %q", errors.String())
			}
		})
	}
}

func TestPublicTestenvDoesNotRegisterPrivilegedWorkerLeaves(t *testing.T) {
	for _, args := range [][]string{
		{"testenv", "worker", "preflight"},
		{"testenv", "worker", "acceptance"},
		{"testenv", "worker", "sweep"},
		{"testenv", "worker", "bootstrap", "plan"},
	} {
		var out, errOut bytes.Buffer
		code := Run(context.Background(), args, &out, &errOut, nil, &fakeConfigLoader{err: context.Canceled})
		if code != 2 || out.Len() != 0 || !strings.Contains(errOut.String(), "Usage:") {
			t.Fatalf("public worker command registered or performed unexpected action: %v code=%d out=%q stderr=%q", args, code, out.String(), errOut.String())
		}
	}
}
