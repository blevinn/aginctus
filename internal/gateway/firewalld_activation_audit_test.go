package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestAuditEgressFirewalldActivationReadOnly(t *testing.T) {
	s := egressTestSpec()
	s.EgressBridgeIPv4CIDR = "10.42.40.1/24"
	s.EgressIPv4Address = "10.42.40.25/24"
	var calls []string
	run := func(_ context.Context, args ...string) (string, error) {
		command := strings.Join(args, " ")
		calls = append(calls, command)
		switch command {
		case "--permanent --get-zones":
			return "nixos-fw-default aginctus-egress", nil
		case "--permanent --get-policies":
			return "aginctus-egress-https", nil
		case "--get-zones":
			return "nixos-fw-default", nil
		case "--get-policies":
			return "", nil
		case "--get-zone-of-interface=aginctus-egress":
			return "no zone", nil
		case "--permanent --info-zone=aginctus-egress":
			return "interfaces: aginctus-egress", nil
		case "--permanent --info-policy=aginctus-egress-https":
			return "target: CONTINUE", nil
		}
		return "", errors.New("unexpected command")
	}
	data, err := s.AuditEgressFirewalldActivation(context.Background(), "nixos-fw-default", run)
	if err != nil {
		t.Fatal(err)
	}
	var got FirewalldActivationAudit
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !got.PermanentZonePresent || !got.PermanentPolicyPresent || got.RuntimeZonePresent || got.RuntimePolicyPresent || got.ActivationApproved {
		t.Fatalf("incorrect activation state: %+v", got)
	}
	for _, call := range calls {
		if strings.Contains(call, "--reload") || strings.Contains(call, "--add-") || strings.Contains(call, "--new-") || strings.Contains(call, "--delete-") {
			t.Fatalf("audit mutated firewall: %s", call)
		}
	}
}

func TestAuditEgressFirewalldActivationFailsClosed(t *testing.T) {
	s := egressTestSpec()
	s.EgressBridgeIPv4CIDR = "10.42.40.1/24"
	s.EgressIPv4Address = "10.42.40.25/24"
	run := func(_ context.Context, args ...string) (string, error) {
		return "", errors.New("firewalld unavailable")
	}
	if _, err := s.AuditEgressFirewalldActivation(context.Background(), "nixos-fw-default", run); err == nil {
		t.Fatal("ignored inspection failure")
	}
}
