package gateway

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"
)

// CheckStagedEgressFirewalld validates the content of permanent firewall
// objects against the current gateway configuration. It never reloads or
// modifies the runtime firewall. A successful check is not reload approval.
func (s Spec) CheckStagedEgressFirewalld(ctx context.Context, uplink string, run FirewalldExecutor) error {
	if run == nil {
		return fmt.Errorf("firewalld executor required")
	}
	if _, err := s.PlanEgressFirewalld(uplink); err != nil {
		return err
	}
	if s.EgressNetwork != egressFirewallZone || uplink == egressFirewallZone || uplink == "aginctus-dns" {
		return fmt.Errorf("unexpected egress bridge or uplink zone")
	}
	zone, err := run(ctx, "--permanent", "--verbose", "--info-zone="+egressFirewallZone)
	if err != nil {
		return fmt.Errorf("read permanent egress zone: %w", err)
	}
	policy, err := run(ctx, "--permanent", "--verbose", "--info-policy="+egressFirewallPolicy)
	if err != nil {
		return fmt.Errorf("read permanent egress policy: %w", err)
	}
	for _, item := range []struct{ output, line, label string }{
		{zone, "description: " + egressFirewallOwnership, "zone ownership"},
		{zone, "target: default", "zone target"},
		{zone, "interfaces: " + s.EgressNetwork, "zone interface"},
		{zone, "ingress-priority: 0", "zone ingress priority"},
		{zone, "egress-priority: 0", "zone egress priority"},
		{policy, "description: " + egressFirewallOwnership, "policy ownership"},
		{policy, "target: CONTINUE", "policy target"},
		{policy, "priority: -1000", "policy priority"},
		{policy, "disable: no", "policy enabled state"},
		{policy, "ingress-zones: " + egressFirewallZone, "policy ingress"},
		{policy, "egress-zones: " + uplink, "policy egress"},
	} {
		if !containsExactLine(item.output, item.line) {
			return fmt.Errorf("staged firewall %s does not match expected configuration", item.label)
		}
	}
	// Fail closed on unexpected services, source binding, forwarded ports,
	// masquerading or an enabled zone-level forward.
	if !fieldTokensEqual(zone, "services:", []string{"dns", "dhcp"}) ||
		!fieldTokensEqual(zone, "interfaces:", []string{s.EgressNetwork}) ||
		!fieldTokensEqual(zone, "sources:", nil) ||
		!fieldTokensEqual(zone, "ports:", nil) ||
		!fieldTokensEqual(zone, "forward-ports:", nil) ||
		!fieldTokensEqual(zone, "protocols:", nil) ||
		!fieldTokensEqual(zone, "source-ports:", nil) ||
		!fieldTokensEqual(zone, "icmp-blocks:", nil) ||
		!containsExactLine(zone, "icmp-block-inversion: no") ||
		!containsExactLine(zone, "masquerade: no") ||
		!containsExactLine(zone, "forward: no") {
		return fmt.Errorf("staged firewall zone has unexpected permissions or bindings")
	}
	if !fieldTokensEqual(policy, "ingress-zones:", []string{egressFirewallZone}) ||
		!fieldTokensEqual(policy, "egress-zones:", []string{uplink}) ||
		!fieldTokensEqual(policy, "services:", nil) ||
		!fieldTokensEqual(policy, "ports:", nil) ||
		!fieldTokensEqual(policy, "protocols:", nil) ||
		!fieldTokensEqual(policy, "source-ports:", nil) ||
		!fieldTokensEqual(policy, "icmp-blocks:", nil) ||
		!containsExactLine(policy, "masquerade: no") ||
		!fieldTokensEqual(policy, "forward-ports:", nil) {
		return fmt.Errorf("staged firewall policy has unexpected permissions or zones")
	}
	zoneRules, err := run(ctx, "--permanent", "--zone="+egressFirewallZone, "--list-rich-rules")
	if err != nil {
		return fmt.Errorf("read staged zone rules: %w", err)
	}
	if strings.TrimSpace(zoneRules) != "" {
		return fmt.Errorf("staged firewall zone has unexpected rich rules")
	}
	// info output renders forward ports on continuation lines. The empty
	// header alone cannot prove the absence of destination NAT grants.
	for _, object := range []string{"--zone=" + egressFirewallZone, "--policy=" + egressFirewallPolicy} {
		ports, err := run(ctx, "--permanent", object, "--list-forward-ports")
		if err != nil {
			return fmt.Errorf("read staged forward ports: %w", err)
		}
		if strings.TrimSpace(ports) != "" {
			return fmt.Errorf("staged firewall object has unexpected forward ports")
		}
	}
	prefix, _ := netip.ParsePrefix(s.EgressIPv4Address)
	source := prefix.Addr().String() + "/32"
	accept := fmt.Sprintf(`rule family="ipv4" priority="-200" source address="%s" port port="443" protocol="tcp" accept`, source)
	// The exact rich rules are queried through the structured CLI interface
	// rather than relying on firewalld's multiline human-readable info format.
	rules, err := run(ctx, "--permanent", "--policy="+egressFirewallPolicy, "--list-rich-rules")
	if err != nil {
		return fmt.Errorf("read staged policy rules: %w", err)
	}
	expected := append([]string{}, egressDenyRules(source)...)
	expected = append(expected, accept)
	if !equalLineSets(rules, expected) {
		return fmt.Errorf("staged firewall rich rules differ from reviewed source-specific HTTPS policy")
	}
	return nil
}

func egressDenyRules(source string) []string {
	subnets := []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "127.0.0.0/8", "100.64.0.0/10", "0.0.0.0/8", "224.0.0.0/4", "240.0.0.0/4"}
	rules := make([]string, 0, len(subnets))
	for _, subnet := range subnets {
		rules = append(rules, fmt.Sprintf(`rule family="ipv4" priority="-300" source address="%s" destination address="%s" drop`, source, subnet))
	}
	return rules
}

func containsExactLine(output, line string) bool {
	for _, candidate := range strings.Split(output, "\n") {
		if strings.TrimSpace(candidate) == line {
			return true
		}
	}
	return false
}
func fieldTokensEqual(output, field string, expected []string) bool {
	for _, candidate := range strings.Split(output, "\n") {
		candidate = strings.TrimSpace(candidate)
		if strings.HasPrefix(candidate, field) {
			got := strings.Fields(strings.TrimSpace(strings.TrimPrefix(candidate, field)))
			if len(got) != len(expected) {
				return false
			}
			slices.Sort(got)
			want := slices.Clone(expected)
			slices.Sort(want)
			return slices.Equal(got, want)
		}
	}
	return false
}
func equalLineSets(output string, expected []string) bool {
	seen := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) != "" {
			seen[strings.TrimSpace(line)]++
		}
	}
	if len(seen) != len(expected) {
		return false
	}
	for _, line := range expected {
		if seen[line] != 1 {
			return false
		}
	}
	return true
}

// UnstageEgressFirewalld removes only an exact, ownership-verified permanent
// staging configuration. It refuses drift and does not reload the firewall.
// On partial failure, the caller must inspect the permanent objects.
func (s Spec) UnstageEgressFirewalld(ctx context.Context, uplink string, run FirewalldExecutor) error {
	if err := s.CheckStagedEgressFirewalld(ctx, uplink, run); err != nil {
		return err
	}
	// Permanent deletion cannot revoke live access. Refuse active or partially
	// loaded objects rather than leaving an orphaned runtime policy behind.
	for _, query := range []struct{ flag, name string }{
		{"--get-zones", egressFirewallZone},
		{"--get-policies", egressFirewallPolicy},
	} {
		names, err := run(ctx, query.flag)
		if err != nil {
			return fmt.Errorf("inspect runtime objects before unstage: %w", err)
		}
		if slices.Contains(strings.Fields(names), query.name) {
			return fmt.Errorf("cannot unstage loaded runtime object %q; withdraw runtime access through the host firewall lifecycle first", query.name)
		}
	}
	if _, err := run(ctx, "--permanent", "--delete-policy="+egressFirewallPolicy); err != nil {
		return fmt.Errorf("delete staged egress policy: %w", err)
	}
	if _, err := run(ctx, "--permanent", "--delete-zone="+egressFirewallZone); err != nil {
		return fmt.Errorf("delete staged egress zone (policy already removed): %w", err)
	}
	return nil
}
