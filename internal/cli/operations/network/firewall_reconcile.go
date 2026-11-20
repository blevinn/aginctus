package network

import (
	"context"
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/host/net/firewall"
)

// FirewallActions provides exactly the privileged capabilities required to
// reconcile the selected host firewall. The caller supplies the root EUID
// check, subprocess runner, and verified bridge-policy discovery.
type FirewallActions struct {
	EffectiveUID func() int
	Command      func(context.Context, string, string, ...string) (string, error)
	Discover     func(context.Context, *config.Config) (firewall.Policy, error)
}

// Reconcile requires verified host authority and backend ownership before
// changing any firewall tables; discovery failures retain fail-closed teardown.
func Reconcile(ctx context.Context, stdout, stderr io.Writer, cfg *config.Config, actions FirewallActions) int {
	if actions.EffectiveUID() != 0 {
		fmt.Fprintln(stderr, "firewall reconciliation requires root (host INPUT mutation)")
		return 1
	}
	ruleset, err := actions.Command(ctx, "", "nft", "-a", "list", "ruleset")
	if err != nil {
		fmt.Fprintln(stderr, "cannot verify effective nftables input backend")
		return 1
	}
	// The NixOS service pins its intended backend. A firewalld restart may
	// temporarily erase its nftables table before startup is complete; do
	// not misdetect that interval as an iptables-nft installation.
	requestedBackend := ""
	if value, exists := cfg.Get("firewall.backend"); exists {
		var ok bool
		requestedBackend, ok = value.(string)
		if !ok {
			fmt.Fprintln(stderr, "firewall backend selection must be a string")
			return 1
		}
	}
	selectedBackend, err := SelectFirewallBackend(ruleset, requestedBackend)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if selectedBackend == "firewalld" {
		return ReconcileFirewalld(ctx, stdout, stderr, cfg, actions)
	}
	if selectedBackend == "nixos-nft" {
		return ReconcileNixosNft(ctx, stdout, stderr, cfg, actions)
	}
	iface, err := firewall.ReconcileOwnedIptablesNft(ctx, firewall.IptablesNftReconcileActions{
		EffectiveUID: actions.EffectiveUID,
		Command: actions.Command,
		Discover: func(ctx context.Context) (firewall.Policy, error) {
			return actions.Discover(ctx, cfg)
		},
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "Aginctus INPUT policy reconciled for verified bridge %q (DNS/DHCP only)\n", iface)
	return 0
}

// Compatibility adapter: backend evidence is owned by host/net/firewall.
func SelectFirewallBackend(ruleset, requested string) (string, error) {
	return firewall.SelectFirewallBackend(ruleset, requested)
}

// reconcileFirewalld uses firewalld's own D-Bus management client. Its
// nftables table has the owner flag: CAP_NET_ADMIN cannot safely modify it
// through nft. The declarative zone has a DROP target and no permanent grants.
// ReconcileFirewalld preserves the existing CLI presentation contract while
// the host firewall subsystem owns authorization and D-Bus reconciliation.
func ReconcileFirewalld(ctx context.Context, stdout, stderr io.Writer, cfg *config.Config, actions FirewallActions) int {
	zone, err := cfg.String("firewall.zone")
	if err != nil {
		fmt.Fprintln(stderr, "invalid configured Aginctus firewalld zone")
		return 1
	}
	result, err := firewall.ReconcileOwnedFirewalld(ctx, zone, firewall.FirewalldReconcileActions{
		EffectiveUID: actions.EffectiveUID,
		Command: actions.Command,
		Discover: func(ctx context.Context) (firewall.Policy, error) {
			return actions.Discover(ctx, cfg)
		},
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if result.AlreadyReconciled {
		fmt.Fprintf(stdout, "firewalld zone already reconciled for bridge %q\n", result.Interface)
	} else {
		fmt.Fprintf(stdout, "firewalld zone reconciled for verified bridge %q (DNS/DHCP only)\n", result.Interface)
	}
	return 0
}

// Existing CLI callers retain these adapters while their tests migrate.
func ContainsWord(words, target string) bool { return firewall.ContainsWord(words, target) }
func SameRichRules(current string, expected []string) bool { return firewall.SameRichRules(current, expected) }
func HasFirewalldDropTarget(details string) bool { return firewall.HasFirewalldDropTarget(details) }

// reconcileNixosNft updates only the child chain predeclared by the NixOS
// module and reached from its effective input-allow decision path.
// ReconcileNixosNft translates the host-owned typed transaction into the
// preexisting CLI output and exit-code contract.
func ReconcileNixosNft(ctx context.Context, stdout, stderr io.Writer, cfg *config.Config, actions FirewallActions) int {
	iface, err := firewall.ReconcileOwnedNixosNft(ctx, firewall.NixosNftReconcileActions{
		EffectiveUID: actions.EffectiveUID,
		Command: actions.Command,
		Discover: func(ctx context.Context) (firewall.Policy, error) {
			return actions.Discover(ctx, cfg)
		},
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "NixOS nftables INPUT reconciled for verified bridge %q (DNS/DHCP only)\n", iface)
	return 0
}
