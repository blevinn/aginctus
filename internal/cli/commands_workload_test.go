package cli

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestCobraWorkloadLegacyRewrite(t *testing.T) {
	for _, tc := range []struct {
		in, out []string
	}{
		{[]string{"workload", "dev", "ensure", "--dry-run"}, []string{"workload", "apply", "dev", "--dry-run"}},
		{[]string{"workload", "dev", "teardown"}, []string{"workload", "remove", "dev"}},
		{[]string{"workload", "apply", "dev"}, []string{"workload", "apply", "dev"}},
		{[]string{"workload", "dev", "run"}, []string{"workload", "dev", "run"}},
	} {
		if got := normalizeWorkloadLegacyArgs(tc.in); !reflect.DeepEqual(got, tc.out) {
			t.Fatalf("got %q, want %q", got, tc.out)
		}
	}
}

func TestCobraWorkloadInvalidSyntaxHasNoConfigAccess(t *testing.T) {
	for _, args := range [][]string{
		{"workload", "status", "a", "b"},
		{"workload", "apply", "--project=aginctus", "dev"},
		{"workload", "apply", "--project=aginctus", "dev", "--render", "--dry-run"},
		{"workload", "start", "--project=aginctus", "dev"},
		{"workload", "stop", "--confirm-stop", "dev"},
		{"workload", "gateway", "provision", "dev", "--model=alias"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var out, errors bytes.Buffer
			code := Run(context.Background(), args, &out, &errors, nil, &fakeConfigLoader{err: context.Canceled})
			if code != 2 || out.Len() != 0 || !strings.Contains(errors.String(), "Usage:") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errors.String())
			}
			if strings.Contains(errors.String(), "configuration:") {
				t.Fatalf("configuration accessed for invalid syntax: %q", errors.String())
			}
		})
	}
}
