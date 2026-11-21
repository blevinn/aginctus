package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	networkops "github.com/blevinn/aginctus/internal/cli/operations/network"
	"github.com/blevinn/aginctus/internal/host/net/firewall"
)

func firewallReconcileFixture(t *testing.T) (*config.Config, *networkops.FirewallActions) {
	t.Helper()
	loader := config.NewLoader()
	loader.SystemPath, loader.UserPath, loader.Environment = "", "", nil
	cfg, err := loader.Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	actions := &networkops.FirewallActions{EffectiveUID: func() int { return 0 }}
	return cfg, actions
}

func TestFirewalldReconcileUsesOwnedZoneAndMinimalRules(t *testing.T) {
	cfg, actions := firewallReconcileFixture(t)
	var grants []string
	actions.Command = func(_ context.Context, input, name string, args ...string) (string, error) {
		call := name + " " + strings.Join(args, " ")
		switch call {
		case "nft -a list ruleset":
			return "table inet firewalld { flags owner,persist; }", nil
		case "firewall-cmd --get-zones":
			return "public aginctus-dns", nil
		case "firewall-cmd --permanent --zone=aginctus-dns --get-target":
			return "DROP", nil
		case "firewall-cmd --zone=aginctus-dns --list-all":
			return "aginctus-dns\n  target: DROP\n", nil
		case "firewall-cmd --zone=aginctus-dns --list-services",
			"firewall-cmd --zone=aginctus-dns --list-ports",
			"firewall-cmd --zone=aginctus-dns --list-sources",
			"firewall-cmd --zone=aginctus-dns --list-protocols",
			"firewall-cmd --zone=aginctus-dns --list-forward-ports",
			"firewall-cmd --zone=aginctus-dns --list-rich-rules",
			"firewall-cmd --zone=aginctus-dns --list-interfaces":
			return "", nil
		default:
			if strings.HasPrefix(call, "firewall-cmd --zone=aginctus-dns --add-rich-rule=") ||
				call == "firewall-cmd --zone=aginctus-dns --add-interface=custom-bridge" {
				if input != "" {
					t.Fatal("firewalld API must not receive nft transaction data")
				}
				grants = append(grants, call)
				return "", nil
			}
			t.Fatalf("unexpected host command %q", call)
			return "", nil
		}
	}
	actions.Discover = func(context.Context, *config.Config) (firewall.Policy, error) {
		return firewall.Policy{Interface: "custom-bridge", Address: "10.42.9.1", Subnet: "10.42.9.0/24"}, nil
	}
	var out, stderr bytes.Buffer
	if rc := runNetworkFirewallReconcileWithActions(context.Background(), &out, &stderr, cfg, *actions); rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if len(grants) != 4 {
		t.Fatalf("expected 3 rich rules and interface binding: %v", grants)
	}
	var dns, dhcp, binding int
	for _, grant := range grants {
		if strings.Contains(grant, `port="53"`) {
			if !strings.Contains(grant, `source address="10.42.9.0/24"`) ||
				!strings.Contains(grant, `destination address="10.42.9.1"`) {
				t.Fatalf("DNS not constrained to bridge subnet/address: %s", grant)
			}
			dns++
		}
		if strings.Contains(grant, `port="67"`) {
			dhcp++
		}
		if strings.Contains(grant, "--add-interface=custom-bridge") {
			binding++
		}
		if strings.Contains(grant, "--permanent") || strings.Contains(grant, "--reload") ||
			strings.Contains(grant, "dport 22") {
			t.Fatalf("unexpected host mutation: %s", grant)
		}
	}
	if dns != 2 || dhcp != 1 || binding != 1 {
		t.Fatalf("unexpected grants: %v", grants)
	}
}

func TestFirewalldDisownedNetworkRemovesRuntimeExceptions(t *testing.T) {
	cfg, actions := firewallReconcileFixture(t)
	existing := `rule family="ipv4" source address="10.42.9.0/24" destination address="10.42.9.1" port port="53" protocol="udp" accept`
	var removals []string
	actions.Command = func(_ context.Context, _ string, name string, args ...string) (string, error) {
		call := name + " " + strings.Join(args, " ")
		switch call {
		case "nft -a list ruleset":
			return "table inet firewalld { flags owner,persist; }", nil
		case "firewall-cmd --get-zones":
			return "aginctus-dns", nil
		case "firewall-cmd --permanent --zone=aginctus-dns --get-target":
			return "DROP", nil
		case "firewall-cmd --zone=aginctus-dns --list-all":
			return "aginctus-dns\n  target: DROP\n", nil
		case "firewall-cmd --zone=aginctus-dns --list-rich-rules":
			return existing, nil
		case "firewall-cmd --zone=aginctus-dns --list-interfaces":
			return "custom-bridge", nil
		case "firewall-cmd --zone=aginctus-dns --list-services",
			"firewall-cmd --zone=aginctus-dns --list-ports",
			"firewall-cmd --zone=aginctus-dns --list-sources",
			"firewall-cmd --zone=aginctus-dns --list-protocols",
			"firewall-cmd --zone=aginctus-dns --list-forward-ports":
			return "", nil
		default:
			if strings.HasPrefix(call, "firewall-cmd --zone=aginctus-dns --remove-rich-rule=") {
				removals = append(removals, call)
				return "", nil
			}
			t.Fatalf("unexpected command after disown: %q", call)
			return "", nil
		}
	}
	actions.Discover = func(context.Context, *config.Config) (firewall.Policy, error) {
		return firewall.Policy{}, errors.New("network ownership mismatch")
	}
	var out, stderr bytes.Buffer
	if rc := runNetworkFirewallReconcileWithActions(context.Background(), &out, &stderr, cfg, *actions); rc != 1 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if len(removals) != 1 || !strings.Contains(removals[0], existing) {
		t.Fatalf("failed to clear stale exception: %v", removals)
	}
}

func TestFirewallReconcileRejectsMixedHostBackendsBeforeMutation(t *testing.T) {
	cfg, actions := firewallReconcileFixture(t)
	calls := 0
	actions.Command = func(_ context.Context, _ string, name string, args ...string) (string, error) {
		calls++
		if name != "nft" || strings.Join(args, " ") != "-a list ruleset" {
			t.Fatal("mixed firewall backends must fail before mutation")
		}
		return "table inet firewalld {}\ntable inet nixos-fw {}", nil
	}
	actions.Discover = func(context.Context, *config.Config) (firewall.Policy, error) {
		t.Fatal("backend failure must not discover network policy")
		return firewall.Policy{}, nil
	}
	var out, stderr bytes.Buffer
	if rc := runNetworkFirewallReconcileWithActions(context.Background(), &out, &stderr, cfg, *actions); rc != 1 || calls != 1 {
		t.Fatalf("rc=%d calls=%d", rc, calls)
	}
}

func TestUnprivilegedFirewallReconcileDoesNotExecuteCommands(t *testing.T) {
	cfg, actions := firewallReconcileFixture(t)
	actions.EffectiveUID = func() int { return 1000 }
	actions.Command = func(context.Context, string, string, ...string) (string, error) {
		t.Fatal("unprivileged reconcile must not execute host command")
		return "", nil
	}
	var out, stderr bytes.Buffer
	if rc := runNetworkFirewallReconcileWithActions(context.Background(), &out, &stderr, cfg, *actions); rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
}
