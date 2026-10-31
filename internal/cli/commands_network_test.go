package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCobraNetworkRejectsMalformedCommandsBeforeConfig(t *testing.T) {
	for _, args := range [][]string{
		{"network", "ensure", "--wat"},
		{"network", "apply", "--dry-run", "extra"},
		{"network", "status", "--force"},
		{"network", "firewall", "policy", "--force"},
		{"network", "firewall", "egress", "revoke", "oops"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			loader := &fakeConfigLoader{err: context.Canceled}
			var out, errOut bytes.Buffer
			code := Run(context.Background(), args, &out, &errOut, nil, loader)
			if code != 2 || out.Len() != 0 || !strings.Contains(errOut.String(), "Usage:") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
			if strings.Contains(errOut.String(), "configuration:") {
				t.Fatalf("configuration accessed on syntax failure: %q", errOut.String())
			}
		})
	}
}
