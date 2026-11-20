package firewall

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestOwnedNixosNftRequiresHostAuthorityBeforeIO(t *testing.T) {
	reads := 0
	_, err := ReconcileOwnedNixosNft(context.Background(), NixosNftReconcileActions{
		EffectiveUID: func() int { return 1000 },
		Command: func(context.Context, string, string, ...string) (string, error) {
			reads++
			return "", nil
		},
		Discover: func(context.Context) (Policy, error) {
			reads++
			return Policy{}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "requires root") || reads != 0 {
		t.Fatalf("authority refusal: err=%v reads=%d", err, reads)
	}
}

func TestOwnedNixosNftRejectsForeignTopologyBeforeMutation(t *testing.T) {
	mutations := 0
	_, err := ReconcileOwnedNixosNft(context.Background(), NixosNftReconcileActions{
		EffectiveUID: func() int { return 0 },
		Command: func(_ context.Context, _ string, _ string, args ...string) (string, error) {
			joined := strings.Join(args, " ")
			switch joined {
			case "-a list chain inet nixos-fw input":
				return "type filter hook input priority filter; policy drop; jump input-allow", nil
			case "-a list chain inet nixos-fw input-allow":
				return "chain input-allow { jump some-foreign-chain }", nil
			default:
				mutations++
				return "", nil
			}
		},
		Discover: func(context.Context) (Policy, error) {
			t.Fatal("unowned firewall was inspected for mutation")
			return Policy{}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "hook missing") || mutations != 0 {
		t.Fatalf("unowned chain: err=%v mutations=%d", err, mutations)
	}
}

func TestOwnedNixosNftClearsOnlyOwnedChainOnDiscoveryFailure(t *testing.T) {
	var txn string
	_, err := ReconcileOwnedNixosNft(context.Background(), NixosNftReconcileActions{
		EffectiveUID: func() int { return 0 },
		Command: func(_ context.Context, input, _ string, args ...string) (string, error) {
			switch strings.Join(args, " ") {
			case "-a list chain inet nixos-fw input":
				return "type filter hook input priority filter; policy drop; jump input-allow", nil
			case "-a list chain inet nixos-fw input-allow":
				return "jump " + NixosNftChain, nil
			case "list chain inet nixos-fw " + NixosNftChain:
				return "chain " + NixosNftChain + " { }", nil
			case "-f -":
				txn = input
				return "", nil
			default:
				t.Fatal("unexpected nftables command")
				return "", nil
			}
		},
		Discover: func(context.Context) (Policy, error) {
			return Policy{}, errors.New("network ownership unverified")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "fail closed") ||
		txn != "flush chain inet nixos-fw "+NixosNftChain+"\n" {
		t.Fatalf("unverified bridge: err=%v transaction=%q", err, txn)
	}
}
