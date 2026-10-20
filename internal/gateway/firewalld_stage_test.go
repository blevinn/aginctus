package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestStageEgressFirewalldNoReload(t *testing.T) {
	s := egressTestSpec()
	s.EgressBridgeIPv4CIDR = "10.42.40.1/24"
	s.EgressIPv4Address = "10.42.40.25/24"
	var commands []string
	fake := func(_ context.Context, args ...string) (string, error) {
		str := strings.Join(args, " ")
		commands = append(commands, str)
		switch str {
		case "--permanent --get-zones":
			return "public nixos-fw-default aginctus-dns", nil
		case "--permanent --get-policies":
			return "aginctus-in aginctus-out", nil
		case "--get-zone-of-interface=aginctus-egress":
			return "no zone", nil
		}
		return "success", nil
	}
	if err := s.StageEgressFirewalld(context.Background(), "nixos-fw-default", fake); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(commands, "\n")
	for _, want := range []string{"--new-zone=aginctus-egress", "--new-policy=aginctus-egress-https", "--add-interface=aginctus-egress", "--add-ingress-zone=aginctus-egress", "--add-egress-zone=nixos-fw-default", `source address="10.42.40.25/32"`, `destination address="10.0.0.0/8"`} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, bad := range []string{"--reload", "--runtime-to-permanent", "--add-service=https", "--set-target=ACCEPT"} {
		if strings.Contains(all, bad) {
			t.Errorf("unsafe command %q", bad)
		}
	}
}

func TestStageEgressFirewalldRefusesExistingZone(t *testing.T) {
	s := egressTestSpec()
	s.EgressBridgeIPv4CIDR = "10.42.40.1/24"
	s.EgressIPv4Address = "10.42.40.25/24"
	calls := 0
	fake := func(_ context.Context, args ...string) (string, error) {
		calls++
		if strings.Contains(strings.Join(args, " "), "--get-zones") {
			return "aginctus-egress nixos-fw-default", nil
		}
		return "", errors.New("unexpected call")
	}
	if err := s.StageEgressFirewalld(context.Background(), "nixos-fw-default", fake); err == nil {
		t.Fatal("accepted existing zone")
	}
	if calls != 1 {
		t.Fatalf("unexpected mutation: %d calls", calls)
	}
}

func TestStageEgressFirewalldRollback(t *testing.T) {
	s := egressTestSpec()
	s.EgressBridgeIPv4CIDR = "10.42.40.1/24"
	s.EgressIPv4Address = "10.42.40.25/24"
	var calls []string
	fake := func(_ context.Context, args ...string) (string, error) {
		command := strings.Join(args, " ")
		calls = append(calls, command)
		switch command {
		case "--permanent --get-zones":
			return "nixos-fw-default", nil
		case "--permanent --get-policies":
			return "", nil
		case "--get-zone-of-interface=aginctus-egress":
			return "no zone", nil
		}
		if strings.Contains(command, "--add-ingress-zone=") {
			return "", errors.New("simulated failure")
		}
		return "success", nil
	}
	if err := s.StageEgressFirewalld(context.Background(), "nixos-fw-default", fake); err == nil {
		t.Fatal("expected staging failure")
	}
	all := strings.Join(calls, "\n")
	for _, want := range []string{"--delete-policy=aginctus-egress-https", "--delete-zone=aginctus-egress"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing rollback %s", want)
		}
	}
}
