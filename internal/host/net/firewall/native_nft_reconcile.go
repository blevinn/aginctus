package firewall

import (
	"context"
	"fmt"
	"strings"
)

// NixosNftReconcileActions are the narrowly scoped host capabilities required
// for owned NixOS nftables child-chain reconciliation. The CLI supplies host
// authority and an independently verified bridge policy; this function does
// not trust an arbitrary bridge name, root-owned state claim or CLI flag.
type NixosNftReconcileActions struct {
	EffectiveUID func() int
	Command func(context.Context, string, string, ...string) (string, error)
	Discover func(context.Context) (Policy, error)
}

// ReconcileOwnedNixosNft changes only the predeclared Aginctus child chain
// reachable from the verified NixOS host INPUT allow path. It returns a typed
// bridge identity; the caller owns all human output and process exit codes.
// Denied or ambiguous host topology fails before any mutation.
func ReconcileOwnedNixosNft(ctx context.Context, actions NixosNftReconcileActions) (string, error) {
	if actions.EffectiveUID == nil || actions.EffectiveUID() != 0 {
		return "", fmt.Errorf("firewall reconciliation requires root (host INPUT mutation)")
	}
	if actions.Command == nil || actions.Discover == nil {
		return "", fmt.Errorf("missing host firewall inspection capabilities")
	}
	base, err := actions.Command(ctx, "", "nft", "-a", "list", "chain", "inet", "nixos-fw", "input")
	if err != nil || !strings.Contains(base, "hook input") ||
		!strings.Contains(base, "policy drop") || !strings.Contains(base, "input-allow") {
		return "", fmt.Errorf("unsupported NixOS nftables INPUT chain")
	}
	allow, err := actions.Command(ctx, "", "nft", "-a", "list", "chain", "inet", "nixos-fw", "input-allow")
	if err != nil || !strings.Contains(allow, "jump "+NixosNftChain) {
		return "", fmt.Errorf("Aginctus hook missing from NixOS nftables input-allow")
	}
	if _, err := actions.Command(ctx, "", "nft", "list", "chain", "inet", "nixos-fw", NixosNftChain); err != nil {
		return "", fmt.Errorf("Aginctus nftables child chain missing; activate the NixOS module")
	}
	policy, discoveryErr := actions.Discover(ctx)
	var approved *Policy
	if discoveryErr == nil {
		approved = &policy
	}
	txn, err := RenderNixosNft(approved)
	if err != nil {
		return "", fmt.Errorf("invalid verified NixOS nftables policy")
	}
	if _, err := actions.Command(ctx, txn, "nft", "-f", "-"); err != nil {
		return "", fmt.Errorf("atomic NixOS nftables child-chain reconciliation failed")
	}
	if discoveryErr != nil {
		return "", fmt.Errorf("Aginctus NixOS nftables exceptions cleared (fail closed): %v", discoveryErr)
	}
	return policy.Interface, nil
}
