package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/firewall"
)

// Replaceable for unit tests. Never expose raw firewall utility output.
var firewallCommand = func(ctx context.Context, input string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func runNetworkFirewallReconcile(ctx context.Context, stdout, stderr io.Writer, cfg *config.Config) int {
	if os.Geteuid() != 0 {
		fmt.Fprintln(stderr, "firewall reconciliation requires root (host INPUT mutation)")
		return 1
	}
	ruleset, err := firewallCommand(ctx, "", "nft", "-a", "list", "ruleset")
	if err != nil {
		fmt.Fprintln(stderr, "cannot verify effective nftables input backend")
		return 1
	}
	if strings.Contains(ruleset, "table inet firewalld") && strings.Contains(ruleset, "table inet nixos-fw") {
		fmt.Fprintln(stderr, "multiple independently rejecting nftables host firewalls detected; fail closed")
		return 1
	}
	if strings.Contains(ruleset, "table inet firewalld") {
		return reconcileFirewalld(ctx, stdout, stderr, cfg)
	}
	if strings.Contains(ruleset, "table inet nixos-fw") {
		fmt.Fprintln(stderr, "NixOS native nftables backend requires a separate supported integration")
		return 1
	}
	version, err := firewallCommand(ctx, "", "iptables", "--version")
	if err != nil || !strings.Contains(version, "(nf_tables)") {
		fmt.Fprintln(stderr, "unsupported firewall backend: require NixOS iptables-nft")
		return 1
	}
	input, err := firewallCommand(ctx, "", "iptables", "-S", "nixos-fw")
	if err != nil || !strings.Contains(input, "-j "+firewall.Chain) {
		fmt.Fprintln(stderr, "Aginctus nixos-fw hook missing; activate the host NixOS module")
		return 1
	}
	if _, err := firewallCommand(ctx, "", "iptables", "-S", firewall.Chain); err != nil {
		fmt.Fprintln(stderr, "Aginctus firewall chain missing")
		return 1
	}
	policy, discoveryErr := deriveFirewallPolicy(ctx, cfg)
	var approved *firewall.Policy
	if discoveryErr == nil {
		approved = &policy
	}
	transaction, err := firewall.Render(approved)
	if err != nil {
		fmt.Fprintln(stderr, "invalid firewall policy")
		return 1
	}
	// --noflush preserves every foreign chain. The explicit -F clears only
	// our chain; iptables-nft commits its replacement transaction atomically.
	if _, err := firewallCommand(ctx, transaction, "iptables-restore", "-w", "--noflush"); err != nil {
		fmt.Fprintln(stderr, "Aginctus INPUT transaction failed; no unrelated rules were modified")
		return 1
	}
	if discoveryErr != nil {
		fmt.Fprintf(stderr, "Aginctus INPUT exceptions cleared (fail closed): %v\n", discoveryErr)
		return 1
	}
	fmt.Fprintf(stdout, "Aginctus INPUT policy reconciled for verified bridge %q (DNS/DHCP only)\n", policy.Interface)
	return 0
}

// reconcileFirewalld uses firewalld's own D-Bus management client. Its
// nftables table has the owner flag: CAP_NET_ADMIN cannot safely modify it
// through nft. The declarative zone has a DROP target and no permanent grants.
func reconcileFirewalld(ctx context.Context, stdout, stderr io.Writer, cfg *config.Config) int {
	run := func(args ...string) (string, error) {
		output, err := firewallCommand(ctx, "", "firewall-cmd", args...)
		return strings.TrimSpace(output), err
	}
	fail := func(message string) int {
		fmt.Fprintln(stderr, message)
		return 1
	}

	zones, err := run("--get-zones")
	if err != nil || !containsWord(zones, firewall.FirewalldZone) {
		return fail("Aginctus-managed firewalld zone is missing; activate the NixOS module")
	}
	zone := "--zone=" + firewall.FirewalldZone
	target, err := run(zone, "--get-target")
	if err != nil || target != "DROP" {
		return fail("Aginctus firewalld zone must have the DROP target")
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
	// Remove previous exceptions *before* evaluating the fresh bridge, so
	// deleting or disowning the network closes existing permissions.
	for _, rule := range strings.Split(current, "\n") {
		if rule == "" {
			continue
		}
		if _, err := run(zone, "--remove-rich-rule="+rule); err != nil {
			return fail("cannot clear stale Aginctus firewalld exceptions")
		}
	}

	owned, discoveryErr := deriveFirewallPolicy(ctx, cfg)
	if discoveryErr != nil {
		fmt.Fprintf(stderr, "Aginctus firewalld exceptions cleared (fail closed): %v\n", discoveryErr)
		return 1
	}
	rules, err := firewall.FirewalldRules(&owned)
	if err != nil {
		return fail("invalid owned-bridge firewalld policy")
	}
	interfaces, err := run(zone, "--list-interfaces")
	if err != nil {
		return fail("cannot inspect Aginctus firewalld interface bindings")
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
	if !containsWord(interfaces, owned.Interface) {
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

func containsWord(words, target string) bool {
	for _, word := range strings.Fields(words) {
		if word == target {
			return true
		}
	}
	return false
}
