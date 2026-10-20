package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func stagedEgressSpec() Spec {
	s := egressTestSpec()
	s.EgressBridgeIPv4CIDR = "10.42.40.1/24"
	s.EgressIPv4Address = "10.42.40.25/24"
	return s
}

func fakeStagedFirewalld(s Spec, mutated *[]string, corrupt string) FirewalldExecutor {
	prefix := "10.42.40.25/32"
	zone := fmt.Sprintf("aginctus-egress\n  summary: \n  description: %s\n  target: default\n  interfaces: aginctus-egress\n  sources: \n  services: dns dhcp\n  ports: \n  protocols: \n  forward: no\n  masquerade: no\n  forward-ports: \n", egressFirewallOwnership)
	policy := fmt.Sprintf("aginctus-egress-https\n  summary: \n  description: %s\n  priority: -1000\n  target: CONTINUE\n  ingress-zones: aginctus-egress\n  egress-zones: nixos-fw-default\n  services: \n  ports: \n  protocols: \n  forward-ports: \n", egressFirewallOwnership)
	rules := append(egressDenyRules(prefix), fmt.Sprintf(`rule family="ipv4" priority="-200" source address="%s" port port="443" protocol="tcp" accept`, prefix))
	zone += "  source-ports: \n  icmp-blocks: \n  icmp-block-inversion: no\n"
	zone += "  ingress-priority: 0\n  egress-priority: 0\n"
	policy += "  source-ports: \n  icmp-blocks: \n  masquerade: no\n"
	policy += "  disable: no\n"
	return func(_ context.Context, args ...string) (string, error) {
		command := strings.Join(args, " ")
		if strings.Contains(command, "--delete-") {
			*mutated = append(*mutated, command)
			return "success", nil
		}
		switch command {
		case "--permanent --verbose --info-zone=aginctus-egress":
			if corrupt == "service-order" {
				return strings.Replace(zone, "services: dns dhcp", "services: dhcp dns", 1), nil
			}
			return zone, nil
		case "--permanent --verbose --info-policy=aginctus-egress-https":
			if corrupt == "priority" {
				return strings.Replace(policy, "priority: -1000", "priority: 0", 1), nil
			}
			return policy, nil
		case "--permanent --zone=aginctus-egress --list-rich-rules":
			return "", nil
		case "--permanent --zone=aginctus-egress --list-forward-ports", "--permanent --policy=aginctus-egress-https --list-forward-ports":
			return "", nil
		case "--get-zones":
			if corrupt == "runtime-zone" {
				return "public aginctus-egress", nil
			}
			return "public", nil
		case "--get-policies":
			if corrupt == "runtime-policy" {
				return "aginctus-egress-https", nil
			}
			return "", nil
		case "--permanent --policy=aginctus-egress-https --list-rich-rules":
			if corrupt == "rule" {
				rules = rules[:len(rules)-1]
			}
			return strings.Join(rules, "\n"), nil
		default:
			return "", errors.New("unexpected firewall-cmd: " + command)
		}
	}
}

func TestCheckStagedEgressFirewalld(t *testing.T) {
	s := stagedEgressSpec()
	for _, corrupt := range []string{"", "priority", "rule"} {
		var mutations []string
		run := fakeStagedFirewalld(s, &mutations, corrupt)
		err := s.CheckStagedEgressFirewalld(context.Background(), "nixos-fw-default", run)
		if corrupt == "" && err != nil {
			t.Errorf("expected valid staged configuration: %v", err)
		}
		if corrupt != "" && err == nil {
			t.Errorf("accepted drift %s", corrupt)
		}
		if len(mutations) != 0 {
			t.Errorf("read-only verification mutated firewall: %v", mutations)
		}
	}
}
func TestUnstageEgressFirewalldOwnership(t *testing.T) {
	s := stagedEgressSpec()
	for _, corrupt := range []string{"", "rule", "runtime-zone", "runtime-policy"} {
		var mutations []string
		run := fakeStagedFirewalld(s, &mutations, corrupt)
		err := s.UnstageEgressFirewalld(context.Background(), "nixos-fw-default", run)
		if corrupt != "" {
			if err == nil || len(mutations) != 0 {
				t.Fatalf("deleted drifted objects: %v %v", err, mutations)
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			if len(mutations) != 2 || !strings.Contains(mutations[0], "--delete-policy=") || !strings.Contains(mutations[1], "--delete-zone=") {
				t.Fatalf("unexpected cleanup: %v", mutations)
			}
		}
	}
}

func TestStagedEgressPermissionDrift(t *testing.T) {
	s := stagedEgressSpec()
	for _, tc := range []struct{ name, query, old, replacement string }{
		{"zone protocol", "--info-zone=", "protocols: ", "protocols: tcp"},
		{"zone source port", "--info-zone=", "source-ports: ", "source-ports: 443/tcp"},
		{"zone ICMP inversion", "--info-zone=", "icmp-block-inversion: no", "icmp-block-inversion: yes"},
		{"zone rich rule", "--zone=aginctus-egress --list-rich-rules", "", `rule accept`},
		{"zone forward port", "--zone=aginctus-egress --list-forward-ports", "", "port=443:proto=tcp:toport=22:toaddr=10.0.0.1"},
		{"policy forward port", "--policy=aginctus-egress-https --list-forward-ports", "", "port=443:proto=tcp:toport=22:toaddr=10.0.0.1"},
		{"policy protocol", "--info-policy=", "protocols: ", "protocols: tcp"},
		{"policy source port", "--info-policy=", "source-ports: ", "source-ports: 443/tcp"},
		{"policy masquerade", "--info-policy=", "masquerade: no", "masquerade: yes"},
		{"policy disabled", "--info-policy=", "disable: no", "disable: yes"},
		{"zone priority", "--info-zone=", "ingress-priority: 0", "ingress-priority: -1000"},
		{"zone ownership", "--info-zone=", egressFirewallOwnership, "foreign owner"},
		{"policy ownership", "--info-policy=", egressFirewallOwnership, "foreign owner"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mutations []string
			base := fakeStagedFirewalld(s, &mutations, "")
			run := func(ctx context.Context, args ...string) (string, error) {
				out, err := base(ctx, args...)
				if strings.Contains(strings.Join(args, " "), tc.query) {
					out = strings.Replace(out, tc.old, tc.replacement, 1)
				}
				return out, err
			}
			if err := s.UnstageEgressFirewalld(context.Background(), "nixos-fw-default", run); err == nil || len(mutations) != 0 {
				t.Fatalf("drift permitted deletion: err=%v mutations=%v", err, mutations)
			}
		})
	}
}

func TestStagedEgressServiceOrdering(t *testing.T) {
	var mutations []string
	s := stagedEgressSpec()
	if err := s.CheckStagedEgressFirewalld(context.Background(), "nixos-fw-default", fakeStagedFirewalld(s, &mutations, "service-order")); err != nil {
		t.Fatal(err)
	}
}

func TestUnstageEgressFailureBoundaries(t *testing.T) {
	for _, failAt := range []string{"--get-zones", "--get-policies", "--delete-policy=", "--delete-zone="} {
		t.Run(failAt, func(t *testing.T) {
			var mutations []string
			s := stagedEgressSpec()
			base := fakeStagedFirewalld(s, &mutations, "")
			run := func(ctx context.Context, args ...string) (string, error) {
				if strings.Contains(strings.Join(args, " "), failAt) {
					return "", errors.New("unavailable")
				}
				return base(ctx, args...)
			}
			if err := s.UnstageEgressFirewalld(context.Background(), "nixos-fw-default", run); err == nil {
				t.Fatal("accepted command failure")
			}
			want := 0
			if failAt == "--delete-zone=" {
				want = 1
			}
			if len(mutations) != want {
				t.Fatalf("unexpected deletion after failure: %v", mutations)
			}
		})
	}
}
