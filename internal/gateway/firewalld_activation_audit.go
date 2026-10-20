package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// FirewalldActivationAudit is deliberately read-only. Missing runtime objects
// are expected before activation and do not imply readiness to reload.
type FirewalldActivationAudit struct {
	Bridge                 string `json:"bridge"`
	UplinkZone             string `json:"uplink_zone"`
	PermanentZonePresent   bool   `json:"permanent_zone_present"`
	PermanentPolicyPresent bool   `json:"permanent_policy_present"`
	RuntimeZonePresent     bool   `json:"runtime_zone_present"`
	RuntimePolicyPresent   bool   `json:"runtime_policy_present"`
	RuntimeInterfaceZone   string `json:"runtime_interface_zone"`
	PermanentZoneDetail    string `json:"permanent_zone_detail,omitempty"`
	PermanentPolicyDetail  string `json:"permanent_policy_detail,omitempty"`
	RuntimeZoneDetail      string `json:"runtime_zone_detail,omitempty"`
	RuntimePolicyDetail    string `json:"runtime_policy_detail,omitempty"`
	ActivationApproved     bool   `json:"activation_approved"`
	Notice                 string `json:"notice"`
}

// AuditEgressFirewalldActivation inspects live and persistent firewalld state.
// It never returns approval for activation: a safe host-specific reload
// strategy and independent firewall confinement checks are still required.
func (s Spec) AuditEgressFirewalldActivation(ctx context.Context, uplink string, run FirewalldExecutor) ([]byte, error) {
	if run == nil {
		return nil, fmt.Errorf("firewalld executor required")
	}
	if _, err := s.PlanEgressFirewalld(uplink); err != nil {
		return nil, err
	}
	ask := func(args ...string) (string, error) {
		result, err := run(ctx, args...)
		if err != nil {
			return "", fmt.Errorf("inspect firewalld %v: %w", args, err)
		}
		return strings.TrimSpace(result), nil
	}
	pz, err := ask("--permanent", "--get-zones")
	if err != nil {
		return nil, err
	}
	pp, err := ask("--permanent", "--get-policies")
	if err != nil {
		return nil, err
	}
	rz, err := ask("--get-zones")
	if err != nil {
		return nil, err
	}
	rp, err := ask("--get-policies")
	if err != nil {
		return nil, err
	}
	iz, err := ask("--get-zone-of-interface=" + s.EgressNetwork)
	if err != nil {
		return nil, err
	}
	has := func(list, name string) bool {
		for _, v := range strings.Fields(list) {
			if v == name {
				return true
			}
		}
		return false
	}
	a := FirewalldActivationAudit{
		Bridge: s.EgressNetwork, UplinkZone: uplink,
		PermanentZonePresent: has(pz, egressFirewallZone), PermanentPolicyPresent: has(pp, egressFirewallPolicy),
		RuntimeZonePresent: has(rz, egressFirewallZone), RuntimePolicyPresent: has(rp, egressFirewallPolicy),
		RuntimeInterfaceZone: iz,
		Notice:               "Read-only inspection, NOT activation approval. Inspect NixOS ownership, runtime-only rules, and policy semantics before any reload. No firewall changes were made.",
	}
	for _, detail := range []struct {
		present bool
		args    []string
		dst     *string
	}{
		{a.PermanentZonePresent, []string{"--permanent", "--info-zone=" + egressFirewallZone}, &a.PermanentZoneDetail},
		{a.PermanentPolicyPresent, []string{"--permanent", "--info-policy=" + egressFirewallPolicy}, &a.PermanentPolicyDetail},
		{a.RuntimeZonePresent, []string{"--info-zone=" + egressFirewallZone}, &a.RuntimeZoneDetail},
		{a.RuntimePolicyPresent, []string{"--info-policy=" + egressFirewallPolicy}, &a.RuntimePolicyDetail},
	} {
		if detail.present {
			result, err := ask(detail.args...)
			if err != nil {
				return nil, err
			}
			*detail.dst = result
		}
	}
	return json.MarshalIndent(a, "", "  ")
}
