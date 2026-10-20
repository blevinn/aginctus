package gateway

import (
	"context"
	"fmt"
	"net/netip"
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
	zone, err := run(ctx, "--permanent", "--info-zone="+egressFirewallZone)
	if err != nil {
		return fmt.Errorf("read permanent egress zone: %w", err)
	}
	policy, err := run(ctx, "--permanent", "--info-policy="+egressFirewallPolicy)
	if err != nil {
		return fmt.Errorf("read permanent egress policy: %w", err)
	}
	for _, item := range []struct{ output, line, label string }{
		{zone, "description: " + egressFirewallOwnership, "zone ownership"},
		{zone, "target: default", "zone target"},
		{zone, "interfaces: " + s.EgressNetwork, "zone interface"},
		{policy, "description: " + egressFirewallOwnership, "policy ownership"},
		{policy, "target: CONTINUE", "policy target"},
		{policy, "priority: -1000", "policy priority"},
		{policy, "ingress-zones: " + egressFirewallZone, "policy ingress"},
		{policy, "egress-zones: " + uplink, "policy egress"},
	} {
		if !containsExactLine(item.output, item.line) {
			return fmt.Errorf("staged firewall %s does not match expected configuration", item.label)
		}
	}
	for _, svc := range []string{"dns", "dhcp"} {
		if !fieldIncludesToken(zone, "services:", svc) {
			return fmt.Errorf("staged firewall zone lacks %s", svc)
		}
	}
	// Fail closed on unexpected services, source binding, forwarded ports,
	// masquerading or an enabled zone-level forward.
	if !fieldTokensEqual(zone, "services:", []string{"dns", "dhcp"}) ||
		!fieldTokensEqual(zone, "interfaces:", []string{s.EgressNetwork}) ||
		!fieldTokensEqual(zone, "sources:", nil) ||
		!fieldTokensEqual(zone, "ports:", nil) ||
		!fieldTokensEqual(zone, "forward-ports:", nil) ||
		!containsExactLine(zone, "masquerade: no") ||
		!containsExactLine(zone, "forward: no") {
		return fmt.Errorf("staged firewall zone has unexpected permissions or bindings")
	}
	if !fieldTokensEqual(policy, "ingress-zones:", []string{egressFirewallZone}) ||
		!fieldTokensEqual(policy, "egress-zones:", []string{uplink}) ||
		!fieldTokensEqual(policy, "services:", nil) ||
		!fieldTokensEqual(policy, "ports:", nil) ||
		!fieldTokensEqual(policy, "forward-ports:", nil) {
		return fmt.Errorf("staged firewall policy has unexpected permissions or zones")
	}
	prefix, _ := netip.ParsePrefix(s.EgressIPv4Address)
	source := prefix.Addr().String()+"/32"
	accept := fmt.Sprintf(`rule family="ipv4" priority="-200" source address="%s" port port="443" protocol="tcp" accept`, source)
	plan, err := s.PlanEgressFirewalld(uplink)
	if err != nil { return err }
	_ = plan
	// The exact rich rules are queried through the structured CLI interface
	// rather than relying on firewalld's multiline human-readable info format.
	rules, err := run(ctx, "--permanent", "--policy="+egressFirewallPolicy, "--list-rich-rules")
	if err != nil { return fmt.Errorf("read staged policy rules: %w",err) }
	expected := append([]string{}, egressDenyRules(source)...)
	expected = append(expected, accept)
	if !equalLineSets(rules, expected) {
		return fmt.Errorf("staged firewall rich rules differ from reviewed source-specific HTTPS policy")
	}
	return nil
}

func egressDenyRules(source string) []string {
	subnets:=[]string{"10.0.0.0/8","172.16.0.0/12","192.168.0.0/16","169.254.0.0/16","127.0.0.0/8","100.64.0.0/10","0.0.0.0/8","224.0.0.0/4","240.0.0.0/4"}
	rules:=make([]string,0,len(subnets))
	for _,subnet:=range subnets {
		rules=append(rules,fmt.Sprintf(`rule family="ipv4" priority="-300" source address="%s" destination address="%s" drop`,source,subnet))
	}
	return rules
}

func containsExactLine(output, line string) bool {
	for _,candidate:=range strings.Split(output,"\n") {
		if strings.TrimSpace(candidate)==line {return true}
	}
	return false
}
func fieldTokensEqual(output, field string, expected []string) bool {
	for _,candidate:=range strings.Split(output,"\n"){
		candidate=strings.TrimSpace(candidate)
		if strings.HasPrefix(candidate,field){
			got:=strings.Fields(strings.TrimSpace(strings.TrimPrefix(candidate,field)))
			if len(got)!=len(expected){return false}
			for i,v:=range expected {if got[i]!=v{return false}}
			return true
		}
	}
	return false
}
func fieldIncludesToken(output, field, value string) bool {
	for _,candidate:=range strings.Split(output,"\n") {
		candidate=strings.TrimSpace(candidate)
		if strings.HasPrefix(candidate,field) {
			for _,token:=range strings.Fields(strings.TrimSpace(strings.TrimPrefix(candidate,field))) {
				if token==value{return true}
			}
		}
	}
	return false
}
func equalLineSets(output string, expected []string) bool {
	seen:=map[string]int{}
	for _,line:=range strings.Split(strings.TrimSpace(output),"\n"){if strings.TrimSpace(line)!=""{seen[strings.TrimSpace(line)]++}}
	if len(seen)!=len(expected){return false}
	for _,line:=range expected {if seen[line]!=1{return false}}
	return true
}
