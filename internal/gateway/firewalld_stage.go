package gateway

import (
	"context"
	"fmt"
	"net/netip"
	"os/exec"
	"strings"
)

// FirewalldExecutor runs firewall-cmd without invoking a shell.
type FirewalldExecutor func(context.Context, ...string) (string, error)

func SystemFirewalld(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "firewall-cmd", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("firewall-cmd failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

const egressFirewallOwnership = "Aginctus managed LiteLLM egress (issue 363)"
const egressFirewallPolicy = "aginctus-egress-https"
const egressFirewallZone = "aginctus-egress"

// StageEgressFirewalld creates permanent policy definitions only. It never
// reloads firewalld and never changes running firewall policy. It refuses
// existing objects, including ones with matching names, to avoid takeover.
//
// The operation is intentionally non-atomic in firewalld's permanent store;
// if staging fails it deletes only objects successfully created in this call.
// A failed rollback is surfaced so the operator can inspect the objects.
func (s Spec) StageEgressFirewalld(ctx context.Context, uplinkZone string, run FirewalldExecutor) error {
	if run == nil {
		return fmt.Errorf("firewalld executor required")
	}
	if _, err := s.PlanEgressFirewalld(uplinkZone); err != nil {
		return err
	}
	if s.EgressNetwork != egressFirewallZone {
		return fmt.Errorf("staging requires the dedicated %q bridge; got %q", egressFirewallZone, s.EgressNetwork)
	}
	// Never stage an uplink zone that is the management zone or egress itself.
	if uplinkZone == egressFirewallZone || uplinkZone == "aginctus-dns" {
		return fmt.Errorf("unsafe firewalld uplink zone %q", uplinkZone)
	}
	for _, obj := range []struct{ kind, name string }{{"zone", egressFirewallZone}, {"policy", egressFirewallPolicy}} {
		// --permanent --get-... lists only actual stored objects. Fail closed on
		// command errors instead of mistaking inability to inspect for absence.
		arg := "--get-zones"
		if obj.kind == "policy" {
			arg = "--get-policies"
		}
		names, err := run(ctx, "--permanent", arg)
		if err != nil {
			return fmt.Errorf("inspect permanent firewalld %ss: %w", obj.kind, err)
		}
		for _, name := range strings.Fields(names) {
			if name == obj.name {
				return fmt.Errorf("firewalld %s %q already exists; refusing ownership takeover", obj.kind, obj.name)
			}
		}
	}
	// Reject hosts where the target bridge belongs to any existing runtime
	// zone. An unassigned bridge is the expected pilot starting state.
	current, err := run(ctx, "--get-zone-of-interface="+s.EgressNetwork)
	if err == nil && strings.TrimSpace(current) != "no zone" && strings.TrimSpace(current) != "" {
		return fmt.Errorf("bridge %q already assigned to firewalld zone %q", s.EgressNetwork, current)
	}
	// Verify the uplink is an existing permanent zone; a typos must not
	// result in a partially staged policy.
	zones, err := run(ctx, "--permanent", "--get-zones")
	if err != nil {
		return err
	}
	found := false
	for _, z := range strings.Fields(zones) {
		if z == uplinkZone {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("uplink zone %q is not defined in permanent firewalld configuration", uplinkZone)
	}
	if _, err = run(ctx, "--permanent", "--new-zone="+egressFirewallZone); err != nil {
		return err
	}
	zoneCreated := true
	policyCreated := false
	rollback := func(cause error) error {
		var failures []string
		if policyCreated {
			if _, e := run(ctx, "--permanent", "--delete-policy="+egressFirewallPolicy); e != nil {
				failures = append(failures, "policy: "+e.Error())
			}
		}
		if zoneCreated {
			if _, e := run(ctx, "--permanent", "--delete-zone="+egressFirewallZone); e != nil {
				failures = append(failures, "zone: "+e.Error())
			}
		}
		if len(failures) > 0 {
			return fmt.Errorf("%w; rollback incomplete: %s", cause, strings.Join(failures, "; "))
		}
		return cause
	}
	do := func(args ...string) error { _, e := run(ctx, args...); return e }
	for _, args := range [][]string{
		{"--permanent", "--zone=" + egressFirewallZone, "--set-description=" + egressFirewallOwnership},
		{"--permanent", "--zone=" + egressFirewallZone, "--set-target=DROP"},
		{"--permanent", "--zone=" + egressFirewallZone, "--add-interface=" + s.EgressNetwork},
		{"--permanent", "--new-policy=" + egressFirewallPolicy},
	} {
		if e := do(args...); e != nil {
			return rollback(e)
		}
	}
	policyCreated = true
	for _, args := range [][]string{
		{"--permanent", "--policy=" + egressFirewallPolicy, "--set-description=" + egressFirewallOwnership},
		{"--permanent", "--policy=" + egressFirewallPolicy, "--set-priority=-1000"},
		{"--permanent", "--policy=" + egressFirewallPolicy, "--set-target=CONTINUE"},
		{"--permanent", "--policy=" + egressFirewallPolicy, "--add-ingress-zone=" + egressFirewallZone},
		{"--permanent", "--policy=" + egressFirewallPolicy, "--add-egress-zone=" + uplinkZone},
	} {
		if e := do(args...); e != nil {
			return rollback(e)
		}
	}
	// Only the owned LiteLLM source may establish HTTPS. No general
	// --add-service=https: it would permit any future guest on the bridge.
	prefix, _ := netip.ParsePrefix(s.EgressIPv4Address)
	source := prefix.Addr().String() + "/32"
	deny := []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "127.0.0.0/8", "100.64.0.0/10", "0.0.0.0/8", "224.0.0.0/4", "240.0.0.0/4"}
	for _, subnet := range deny {
		rule := fmt.Sprintf(`rule family="ipv4" priority="-300" source address="%s" destination address="%s" drop`, source, subnet)
		if e := do("--permanent", "--policy="+egressFirewallPolicy, "--add-rich-rule="+rule); e != nil {
			return rollback(e)
		}
	}
	accept := fmt.Sprintf(`rule family="ipv4" priority="-200" source address="%s" port port="443" protocol="tcp" accept`, source)
	if e := do("--permanent", "--policy="+egressFirewallPolicy, "--add-rich-rule="+accept); e != nil {
		return rollback(e)
	}
	// Leave both objects in permanent store, unactivated.
	zoneCreated = false
	return nil
}
