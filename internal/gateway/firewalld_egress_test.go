package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPlanEgressFirewalld(t *testing.T) {
	spec := egressTestSpec()
	spec.EgressBridgeIPv4CIDR = "10.42.40.1/24"
	spec.EgressIPv4Address = "10.42.40.25/24"
	data, err := spec.PlanEgressFirewalld("nixos-fw-default")
	if err != nil {
		t.Fatal(err)
	}
	var p EgressFirewalldPlan
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	if p.Interface != "aginctus-egress" || p.UplinkZone != "nixos-fw-default" ||
		p.Source != "10.42.40.25/32" || p.Zone != "aginctus-egress" ||
		p.Policy != "aginctus-egress-https" || p.Target != "CONTINUE" || !p.HTTPS {
		t.Fatalf("incorrect policy scope: %+v", p)
	}
	if len(p.RichRules) != 14 {
		t.Fatalf("missing private-network restrictions: %v", p.RichRules)
	}
	for _, rule := range p.RichRules {
		if !strings.Contains(rule, `source address="10.42.40.25/32"`) ||
			!strings.Contains(rule, ` drop`) {
			t.Fatalf("unsafe rich rule: %q", rule)
		}
	}
	if strings.Contains(string(data), "masquerade") {
		t.Fatal("forwarding plan must leave Incus NAT unchanged")
	}
}

func TestPlanEgressFirewalldRejectsAmbiguity(t *testing.T) {
	for _, uplink := range []string{"", "trusted", "ANY", "HOST", "../oops", "x y"} {
		spec := egressTestSpec()
		spec.EgressBridgeIPv4CIDR = "10.42.40.1/24"
		spec.EgressIPv4Address = "10.42.40.25/24"
		if _, err := spec.PlanEgressFirewalld(uplink); err == nil {
			t.Errorf("accepted unsafe uplink %q", uplink)
		}
	}
	spec := egressTestSpec()
	if _, err := spec.PlanEgressFirewalld("nixos-fw-default"); err == nil {
		t.Fatal("accepted dynamically addressed egress network")
	}
}
