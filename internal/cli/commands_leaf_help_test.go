package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCobraNestedHelpReflectsLeafFlagScope(t *testing.T) {
	for _, tc := range []struct {
		args []string
		required []string
		forbidden []string
	}{
		{
			args: []string{"gateway", "egress", "dhcp", "apply", "--help"},
			required: []string{"--ranges", "--confirm-bridge-only"},
			forbidden: []string{"--confirm-stopped-instance", "--uplink-zone"},
		},
		{
			args: []string{"gateway", "egress", "reconcile", "--help"},
			required: []string{"--dry-run", "--confirm-stopped-instance"},
			forbidden: []string{"--confirm-bridge-only", "--uplink-zone"},
		},
		{
			args: []string{"gateway", "model", "register", "--help"},
			required: []string{"--provider", "--source", "--dry-run"},
			forbidden: []string{"--confirm-bridge-only", "--key-file"},
		},
		{
			args: []string{"images", "import-owned", "--help"},
			required: []string{"--project", "--owner", "--confirm-create"},
			forbidden: []string{"--force", "--confirm-stop"},
		},
		{
			args: []string{"workload", "start", "--help"},
			required: []string{"--project", "--confirm-start"},
			forbidden: []string{"--confirm-stop", "--render"},
		},
	} {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			var out, errors bytes.Buffer
			code := Run(context.Background(), tc.args, &out, &errors, nil, nil)
			if code != 0 || errors.Len() != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errors.String())
			}
			for _, want := range tc.required {
				if !strings.Contains(out.String(), want) {
					t.Errorf("leaf help missing %q: %s", want, out.String())
				}
			}
			for _, forbidden := range tc.forbidden {
				if strings.Contains(out.String(), forbidden) {
					t.Errorf("leaf help leaked unrelated flag %q: %s", forbidden, out.String())
				}
			}
		})
	}
}
