package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/firewall"
	"github.com/lxc/incus/v7/shared/api"
)

func TestEgressLockAndState(t *testing.T) {
	dir := t.TempDir()
	release, err := lockEgress(dir)
	if err != nil {
		t.Fatal(err)
	}
	if second, e := lockEgress(dir); e == nil {
		second()
		t.Fatal("concurrent reconcile acquired lock")
	}
	release()
	release, err = lockEgress(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	file := egressStateFile(filepath.Join(dir, "state.json"))
	state := firewall.EgressState{Current: firewall.EgressGrant{Uplink: "public"}}
	if err = file.Save(state); err != nil {
		t.Fatal(err)
	}
	got, err := file.Load()
	if err != nil || got.Current.Uplink != "public" {
		t.Fatalf("state round trip: %+v %v", got, err)
	}
}

func TestEgressNoUnprivilegedMutationOrRetiredStaging(t *testing.T) {
	old := firewallEffectiveUID
	firewallEffectiveUID = func() int { return 1000 }
	t.Cleanup(func() { firewallEffectiveUID = old })
	var out, errs bytes.Buffer
	if rc := runNetworkFirewallEgress(context.Background(), &out, &errs, nil, false); rc != 1 || !strings.Contains(errs.String(), "requires root") {
		t.Fatalf("rc=%d %s", rc, errs.String())
	}
	for _, command := range []string{"stage", "unstage", "verify-staged"} {
		errs.Reset()
		if rc := runGateway(context.Background(), []string{"egress", "firewall", command}, &out, &errs, nil, nil); rc != 2 || !strings.Contains(errs.String(), "retired") {
			t.Fatalf("rc=%d %s", rc, errs.String())
		}
	}
}

func TestDiscoverEgressIdentityAndRoutes(t *testing.T) {
	oldQuery, oldNetwork, oldCommand, oldInspect := gatewayEgressQuery, getFirewallNetwork, firewallCommand, inspectEgressLink
	oldZone := effectiveEgressZone
	t.Cleanup(func() {
		gatewayEgressQuery, getFirewallNetwork, firewallCommand, inspectEgressLink = oldQuery, oldNetwork, oldCommand, oldInspect
		effectiveEgressZone = oldZone
	})
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	cfg, err := loader.Load(config.Options{Overrides: []string{"gateway.egress.network.name=aginctus-egress", "gateway.egress.network.ipv4.address=10.42.40.1/24", "gateway.egress.ipv4.address=10.42.40.25/24", "firewall.egress.uplinkZone=public"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, drift := range []string{"", "ownership", "NIC", "collision", "unrestricted DHCP", "IPv6", "link", "uplink", "multiple routes", "opaque route", "policy routing", "multipath", "extra NIC", "bridged peer"} {
		t.Run(drift, func(t *testing.T) {
			bridge := map[string]any{"name": "aginctus-egress", "type": "bridge", "managed": true, "config": map[string]string{"user.aginctus.managed": "true", "user.aginctus.resource": "gateway-egress-network", "ipv4.address": "10.42.40.1/24", "ipv4.nat": "true", "ipv4.routing": "true", "ipv4.dhcp": "false", "ipv6.address": "none"}}
			bc := bridge["config"].(map[string]string)
			if drift == "ownership" {
				bc["user.aginctus.managed"] = "false"
			}
			if drift == "unrestricted DHCP" {
				bc["ipv4.dhcp"] = "true"
			}
			if drift == "IPv6" {
				bc["ipv6.address"] = "auto"
			}
			instance := map[string]any{"name": "litellm-1", "config": map[string]string{"user.aginctus.managed": "true", "user.aginctus.resource": "compose-instance", "user.aginctus.deployment": "aginctus-gateway"}, "devices": map[string]map[string]string{"eth0": {"type": "nic", "network": "aginctus-mgmt", "ipv4.gateway": "none"}, "eth1": {"type": "nic", "network": "aginctus-egress", "ipv4.address": "10.42.40.25/24"}}}
			if drift == "NIC" {
				instance["devices"].(map[string]map[string]string)["eth0"]["ipv4.gateway"] = "auto"
			}
			if drift == "extra NIC" {
				instance["devices"].(map[string]map[string]string)["eth2"] = map[string]string{"type": "nic", "network": "other"}
			}
			instance["expanded_devices"] = instance["devices"]
			gatewayEgressQuery = func(_ context.Context, args ...string) ([]byte, error) {
				path := args[len(args)-1]
				var value any
				switch {
				case strings.HasSuffix(path, "/leases"):
					value = []any{}
				case strings.Contains(path, "/networks/"):
					value = bridge
				case strings.Contains(path, "/projects?"):
					value = []any{map[string]string{"name": "aginctus-gateway"}}
				case strings.Contains(path, "/instances?"):
					value = []any{instance}
					if drift == "collision" {
						value = []any{instance, map[string]any{"name": "other", "expanded_devices": instance["devices"]}}
					}
					if drift == "bridged peer" {
						value = []any{instance, map[string]any{"name": "other", "expanded_devices": map[string]map[string]string{"eth0": {"type": "nic", "parent": "aginctus-egress", "ipv4.address": "10.42.40.26/24"}}}}
					}
				case strings.Contains(path, "/instances/litellm-1?"):
					value = instance
				default:
					return nil, errors.New("unexpected Incus query")
				}
				return json.Marshal(value)
			}
			getFirewallNetwork = func(context.Context, string) (*api.Network, error) {
				raw, _ := json.Marshal(bridge)
				var n api.Network
				err := json.Unmarshal(raw, &n)
				return &n, err
			}
			inspectEgressLink = func(string, net.IP) error {
				if drift == "link" {
					return errors.New("link mismatch")
				}
				return nil
			}
			effectiveEgressZone = func(context.Context, string) (string, error) {
				if drift == "uplink" {
					return "trusted", nil
				}
				return "public", nil
			}
			firewallCommand = func(_ context.Context, _ string, name string, args ...string) (string, error) {
				if strings.Join(args, " ") == "-j -4 rule show" {
					if drift == "policy routing" {
						return `[{"priority":0,"src":"all","table":"local"},{"priority":100,"src":"10.42.40.0/24","table":"other"}]`, nil
					}
					return `[{"priority":0,"src":"all","table":"local"},{"priority":32766,"src":"all","table":"main"},{"priority":32767,"src":"all","table":"default"}]`, nil
				}
				if drift == "multipath" {
					return `[{"dst":"default","dev":"wan0","nexthops":[{"dev":"wan1"}]}]`, nil
				}
				if drift == "multiple routes" {
					return `[{"dst":"default","dev":"wan0"},{"dst":"default","dev":"wan1"}]`, nil
				}
				if drift == "opaque route" {
					return `[{"dst":"default","dev":"wan0"},{"dst":"bad"}]`, nil
				}
				return `[{"dst":"default","dev":"wan0"},{"dst":"11.12.13.0/24","dev":"lan0"},{"dst":"11.13.14.15","dev":"lo","type":"local"}]`, nil
			}
			grant, err := discoverEgressGrant(context.Background(), cfg)
			if drift != "" {
				if err == nil {
					t.Fatal("accepted unsafe discovery")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(grant.HTTPS, "\n"), `destination address="11.13.14.15/32" drop`) {
				t.Fatal("host-local public address not excluded")
			}
		})
	}
}
