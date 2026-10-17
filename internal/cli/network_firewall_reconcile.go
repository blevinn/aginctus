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
var firewallEffectiveUID = os.Geteuid
var discoverFirewallPolicy = deriveFirewallPolicy

var firewallCommand = func(ctx context.Context, input string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func runNetworkFirewallReconcile(ctx context.Context, stdout, stderr io.Writer, cfg *config.Config) int {
	if firewallEffectiveUID() != 0 {
		fmt.Fprintln(stderr, "firewall reconciliation requires root (host INPUT mutation)")
		return 1
	}
	ruleset, err := firewallCommand(ctx, "", "nft", "-a", "list", "ruleset")
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
	selectedBackend, err := selectFirewallBackend(ruleset, requestedBackend)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if selectedBackend == "firewalld" {
		return reconcileFirewalld(ctx, stdout, stderr, cfg)
	}
	if selectedBackend == "nixos-nft" {
		return reconcileNixosNft(ctx, stdout, stderr, cfg)
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
	policy, discoveryErr := discoverFirewallPolicy(ctx, cfg)
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

func selectFirewallBackend(ruleset, requested string) (string, error) {
	firewalldPresent := strings.Contains(ruleset, "table inet firewalld")
	nixosPresent := strings.Contains(ruleset, "table inet nixos-fw")
	if firewalldPresent && nixosPresent {
		return "", fmt.Errorf("multiple independently rejecting nftables host firewalls detected; fail closed")
	}
	switch requested {
	case "nixos-nft":
		if !nixosPresent {
			return "", fmt.Errorf("NixOS native nftables input table not ready")
		}
		return "nixos-nft", nil
	case "firewalld":
		if !firewalldPresent {
			return "", fmt.Errorf("firewalld nftables table not ready; retry after firewalld startup")
		}
		return "firewalld", nil
	case "iptables-nft":
		if firewalldPresent || nixosPresent {
			return "", fmt.Errorf("configured iptables-nft backend conflicts with active nftables host firewall")
		}
		return "iptables-nft", nil
	case "":
		// Interactive CLI usage retains read-only automatic selection.
		if firewalldPresent {
			return "firewalld", nil
		}
		if nixosPresent {
			return "nixos-nft", nil
		}
		return "iptables-nft", nil
	default:
		return "", fmt.Errorf("unsupported firewall backend selection")
	}
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

	zoneName, err := cfg.String("firewall.zone")
	if err != nil || firewall.ValidateFirewalldZone(zoneName) != nil {
		return fail("invalid configured Aginctus firewalld zone")
	}
	zones, err := run("--get-zones")
	if err != nil || !containsWord(zones, zoneName) {
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
	if err != nil || !hasFirewalldDropTarget(activeZone) {
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
	owned, discoveryErr := discoverFirewallPolicy(ctx, cfg)
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
	if discoveryErr == nil && sameRichRules(current, rules) &&
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

func sameRichRules(current string, expected []string) bool {
	if strings.TrimSpace(current) == "" {
		return len(expected) == 0
	}
	existing := strings.Split(strings.TrimSpace(current), "\n")
	if len(existing) != len(expected) {
		return false
	}
	for _, rule := range expected {
		found := false
		for _, installed := range existing {
			if strings.TrimSpace(installed) == rule {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func hasFirewalldDropTarget(details string) bool {
	for _, line := range strings.Split(details, "\n") {
		if strings.TrimSpace(line) == "target: DROP" {
			return true
		}
	}
	return false
}

// reconcileNixosNft updates only the child chain predeclared by the NixOS
// module and reached from its effective input-allow decision path.
func reconcileNixosNft(ctx context.Context, stdout, stderr io.Writer, cfg *config.Config) int {
	base, err := firewallCommand(ctx, "", "nft", "-a", "list", "chain", "inet", "nixos-fw", "input")
	if err != nil || !strings.Contains(base, "hook input") ||
		!strings.Contains(base, "policy drop") || !strings.Contains(base, "input-allow") {
		fmt.Fprintln(stderr, "unsupported NixOS nftables INPUT chain")
		return 1
	}
	allow, err := firewallCommand(ctx, "", "nft", "-a", "list", "chain", "inet", "nixos-fw", "input-allow")
	if err != nil || !strings.Contains(allow, "jump "+firewall.NixosNftChain) {
		fmt.Fprintln(stderr, "Aginctus hook missing from NixOS nftables input-allow")
		return 1
	}
	if _, err := firewallCommand(ctx, "", "nft", "list", "chain", "inet", "nixos-fw", firewall.NixosNftChain); err != nil {
		fmt.Fprintln(stderr, "Aginctus nftables child chain missing; activate the NixOS module")
		return 1
	}
	policy, discoveryErr := discoverFirewallPolicy(ctx, cfg)
	var approved *firewall.Policy
	if discoveryErr == nil {
		approved = &policy
	}
	txn, err := firewall.RenderNixosNft(approved)
	if err != nil {
		fmt.Fprintln(stderr, "invalid verified NixOS nftables policy")
		return 1
	}
	if _, err := firewallCommand(ctx, txn, "nft", "-f", "-"); err != nil {
		fmt.Fprintln(stderr, "atomic NixOS nftables child-chain reconciliation failed")
		return 1
	}
	if discoveryErr != nil {
		fmt.Fprintf(stderr, "Aginctus NixOS nftables exceptions cleared (fail closed): %v\n", discoveryErr)
		return 1
	}
	fmt.Fprintf(stdout, "NixOS nftables INPUT reconciled for verified bridge %q (DNS/DHCP only)\n", policy.Interface)
	return 0
}
