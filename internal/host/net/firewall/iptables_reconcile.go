package firewall

import (
	"context"
	"fmt"
	"strings"
)

// IptablesNftReconcileActions provides bounded host capabilities, not CLI
// rendering or an ambient privilege escalation path.
type IptablesNftReconcileActions struct {
	EffectiveUID func() int
	Command func(context.Context, string, string, ...string) (string, error)
	Discover func(context.Context) (Policy, error)
}

// ReconcileOwnedIptablesNft requires the NixOS parent hook and owned child
// chain before emitting a single atomic --noflush iptables-restore transaction.
// A failed bridge observation clears only the Aginctus chain, failing closed.
func ReconcileOwnedIptablesNft(ctx context.Context, actions IptablesNftReconcileActions) (string, error) {
	if actions.EffectiveUID == nil || actions.EffectiveUID() != 0 {
		return "", fmt.Errorf("firewall reconciliation requires root (host INPUT mutation)")
	}
	if actions.Command == nil || actions.Discover == nil {
		return "", fmt.Errorf("missing host firewall inspection capabilities")
	}
	version, err := actions.Command(ctx, "", "iptables", "--version")
	if err != nil || !strings.Contains(version, "(nf_tables)") {
		return "", fmt.Errorf("unsupported firewall backend: require NixOS iptables-nft")
	}
	input, err := actions.Command(ctx, "", "iptables", "-S", "nixos-fw")
	if err != nil || !strings.Contains(input, "-j "+Chain) {
		return "", fmt.Errorf("aginctus nixos-fw hook missing; activate the host NixOS module")
	}
	if _, err := actions.Command(ctx, "", "iptables", "-S", Chain); err != nil {
		return "", fmt.Errorf("aginctus firewall chain missing")
	}
	policy, discoveryErr := actions.Discover(ctx)
	var approved *Policy
	if discoveryErr == nil { approved = &policy }
	transaction, err := Render(approved)
	if err != nil { return "", fmt.Errorf("invalid firewall policy") }
	if _, err := actions.Command(ctx, transaction, "iptables-restore", "-w", "--noflush"); err != nil {
		return "", fmt.Errorf("aginctus INPUT transaction failed; no unrelated rules were modified")
	}
	if discoveryErr != nil {
		return "", fmt.Errorf("aginctus INPUT exceptions cleared (fail closed): %v", discoveryErr)
	}
	return policy.Interface, nil
}
