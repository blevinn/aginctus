package firewall

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestOwnedIptablesNftRejectsUnverifiedHost(t *testing.T) {
	for _, tc := range []struct {
		name, version, hook string
		uid int
		failChain bool
	}{
		{"nonroot", "iptables v1.8 (nf_tables)", "-j AGINCTUS_INPUT", 1000, false},
		{"legacy backend", "iptables v1.8 (legacy)", "-j AGINCTUS_INPUT", 0, false},
		{"missing hook", "iptables v1.8 (nf_tables)", "-j FOREIGN", 0, false},
		{"missing owned chain", "iptables v1.8 (nf_tables)", "-j AGINCTUS_INPUT", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes := 0
			_, err := ReconcileOwnedIptablesNft(context.Background(), IptablesNftReconcileActions{
				EffectiveUID: func() int { return tc.uid },
				Command: func(_ context.Context, input, command string, args ...string) (string, error) {
					if input != "" || command == "iptables-restore" { writes++; return "", nil }
					if command != "iptables" { t.Fatalf("unrecognized command %s", command) }
					if len(args) == 1 && args[0] == "--version" { return tc.version, nil }
					if len(args) == 2 && args[1] == "nixos-fw" { return tc.hook, nil }
					if tc.failChain { return "", errors.New("missing chain") }
					return "-N AGINCTUS_INPUT", nil
				},
				Discover: func(context.Context) (Policy, error) { t.Fatal("discovery on denied host"); return Policy{}, nil },
			})
			if err == nil || writes != 0 { t.Fatalf("err=%v writes=%d", err, writes) }
		})
	}
	if _, err := ReconcileOwnedIptablesNft(context.Background(), IptablesNftReconcileActions{}); err == nil {
		t.Fatal("missing capabilities accepted")
	}
}

func TestOwnedIptablesNftDiscoveryFailureClearsOnlyOwnedChain(t *testing.T) {
	var tx string
	commands := 0
	_, err := ReconcileOwnedIptablesNft(context.Background(), IptablesNftReconcileActions{
		EffectiveUID: func() int { return 0 },
		Command: func(_ context.Context, input, command string, args ...string) (string, error) {
			if command == "iptables-restore" {
				commands++
				if len(args) != 2 || args[0] != "-w" || args[1] != "--noflush" { t.Fatalf("unsafe restore args: %q", args) }
				tx = input
				return "", nil
			}
			if command != "iptables" { t.Fatalf("unexpected %s", command) }
			if len(args) == 1 { return "iptables v1.8 (nf_tables)", nil }
			if args[1] == "nixos-fw" { return "-A nixos-fw -j AGINCTUS_INPUT", nil }
			return "-N AGINCTUS_INPUT", nil
		},
		Discover: func(context.Context) (Policy, error) { return Policy{}, errors.New("missing bridge") },
	})
	if err == nil || !strings.Contains(err.Error(), "fail closed") || commands != 1 {
		t.Fatalf("err=%v transactions=%d", err, commands)
	}
	if tx != "*filter\n:AGINCTUS_INPUT - [0:0]\n-F AGINCTUS_INPUT\nCOMMIT\n" {
		t.Fatalf("unexpected fail-closed transaction: %q", tx)
	}
}
