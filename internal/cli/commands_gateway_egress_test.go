package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestGatewayEgressCobraRequiresExplicitConfirmationsBeforeConfig(t *testing.T) {
	for _, args := range [][]string{
		{"gateway", "egress", "reconcile"},
		{"gateway", "egress", "reconcile", "--confirm-stopped-instance=false"},
		{"gateway", "egress", "reconcile", "--dry-run", "--confirm-stopped-instance"},
		{"gateway", "egress", "dhcp", "apply", "--ranges=10.0.0.10-10.0.0.50"},
		{"gateway", "egress", "dhcp", "apply", "--confirm-bridge-only"},
		{"gateway", "egress", "firewall", "plan"},
		{"gateway", "egress", "firewall", "audit", "--ranges=foo"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := Run(context.Background(), args, &out, &errOut, nil, &fakeConfigLoader{err: context.Canceled})
			if code != 2 || out.Len() != 0 || !strings.Contains(errOut.String(), "Usage:") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
			if strings.Contains(errOut.String(), "configuration:") {
				t.Fatalf("config read on missing confirmation: %q", errOut.String())
			}
		})
	}
}
