package network

import (
	"context"
	"fmt"
	"io"
	"strings"

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
func ReconcileFirewalld(ctx context.Context, stdout, stderr io.Writer, cfg *config.Config, actions FirewallActions) int {
	run := func(args ...string) (string, error) {
		output, err := actions.Command(ctx, "", "firewall-cmd", args...)
		return strings.TrimSpace(output), err
	}
	fail := func(message string) int {
		fmt.Fprintln(stderr, message)
		return 1
	}

	zoneName, err := cfg.String("firewall.zone")
	if err != nil || firewall.ValidateFirewalldZone(zoneName) != nil {
		return fail("invalid configured Aginctus firewalld zone")
	}
	zones, err := run("--get-zones")
	if err != nil || !ContainsWord(zones, zoneName) {
		return fail("configured Aginctus-managed firewalld zone is missing; activate the NixOS module")
	}
	zone := "--zone=" + zoneName
	// firewalld exposes --get-target only in permanent configuration.
	// Also inspect the active runtime zone: a permanent DROP alone does not
	// prove that the currently enforced target is still restrictive.
	target, err := run("--permanent", zone, "--get-target")
	if err != nil || target != "DROP" {
		return fail("Aginctus firewalld permanent zone must have the DROP target")
	}
	activeZone, err := run(zone, "--list-all")
	if err != nil || !HasFirewalldDropTarget(activeZone) {
		return fail("Aginctus firewalld runtime zone must have the DROP target")
	}
	// We own this dedicated zone. Refuse unexpected runtime configuration
	// instead of accidentally extending somebody else's host exceptions.
	for _, flag := range []string{"--list-services", "--list-ports", "--list-sources", "--list-protocols", "--list-forward-ports"} {
		output, err := run(zone, flag)
		if err != nil || output != "" {
			return fail("Aginctus firewalld zone contains unexpected permissions")
		}
	}
	current, err := run(zone, "--list-rich-rules")
	if err != nil {
		return fail("cannot inspect Aginctus firewalld rich rules")
	}
	owned, discoveryErr := actions.Discover(ctx, cfg)
	var rules []string
	if discoveryErr == nil {
		rules, err = firewall.FirewalldRules(&owned)
		if err != nil {
			return fail("invalid owned-bridge firewalld policy")
		}
	}
	interfaces, err := run(zone, "--list-interfaces")
	if err != nil {
		return fail("cannot inspect Aginctus firewalld interface bindings")
	}
	// Leave an unchanged, verified runtime policy intact. The periodic timer
	// must not interrupt active DNS connections every 30 seconds.
	if discoveryErr == nil && SameRichRules(current, rules) &&
		strings.TrimSpace(interfaces) == owned.Interface {
		fmt.Fprintf(stdout, "firewalld zone already reconciled for bridge %q\n", owned.Interface)
		return 0
	}

	// Remove prior exceptions before admitting any new identity. If network
	// discovery failed, this clears old access and leaves the DROP zone.
	for _, rule := range strings.Split(current, "\n") {
		if rule == "" {
			continue
		}
		if _, err := run(zone, "--remove-rich-rule="+rule); err != nil {
			return fail("cannot clear stale Aginctus firewalld exceptions")
		}
	}
	if discoveryErr != nil {
		fmt.Fprintf(stderr, "Aginctus firewalld exceptions cleared (fail closed): %v\n", discoveryErr)
		return 1
	}
	for _, iface := range strings.Fields(interfaces) {
		if iface != owned.Interface {
			return fail("Aginctus firewalld zone binds an unexpected interface; exceptions remain cleared")
		}
	}
	// Add the narrow permissions before binding the verified bridge. The
	// zone is dedicated to Aginctus; no non-owned ingress is admitted.
	for _, rule := range rules {
		if _, err := run(zone, "--add-rich-rule="+rule); err != nil {
			// Best-effort rollback of any partial additions. The DROP zone
			// remains in effect even if the D-Bus backend is unavailable.
			for _, prior := range rules {
				_, _ = run(zone, "--remove-rich-rule="+prior)
			}
			return fail("cannot install Aginctus firewalld DNS/DHCP permissions")
		}
	}
	if !ContainsWord(interfaces, owned.Interface) {
		if _, err := run(zone, "--add-interface="+owned.Interface); err != nil {
			for _, rule := range rules {
				_, _ = run(zone, "--remove-rich-rule="+rule)
			}
			return fail("cannot bind verified Incus bridge to Aginctus firewalld zone")
		}
	}
	fmt.Fprintf(stdout, "firewalld zone reconciled for verified bridge %q (DNS/DHCP only)\n", owned.Interface)
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
