package firewall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"
)

// Busctl uses firewalld's native API with interactive authorization disabled.
// No shell, permanent writes, reload, direct rules or second firewall manager.
type Busctl struct{}

const firewalldBus = "org.fedoraproject.FirewallD1"
const firewalldPath = "/org/fedoraproject/FirewallD1"

// EffectiveZone includes implicit default-zone classification. Reconciliation
// must not require an ad hoc permanent binding for a physical uplink.
func (Busctl) EffectiveZone(ctx context.Context, iface string) (string, error) {
	raw, err := busCall(ctx, firewalldPath, firewalldBus+".zone", "getZoneOfInterface", "s", iface)
	if err != nil {
		return "", err
	}
	var zone string
	if err = json.Unmarshal(raw, &zone); err != nil {
		return "", err
	}
	if zone != "" {
		return zone, nil
	}
	raw, err = busCall(ctx, firewalldPath, firewalldBus, "getDefaultZone")
	if err != nil {
		return "", err
	}
	err = json.Unmarshal(raw, &zone)
	return zone, err
}

func (Busctl) Forget(ctx context.Context, source string) error {
	ip, err := netip.ParseAddr(source)
	if err != nil || !ip.Is4() {
		return fmt.Errorf("invalid HTTPS conntrack source")
	}
	output, err := exec.CommandContext(ctx, "conntrack", "-D", "-f", "ipv4", "-p", "tcp", "--orig-src", ip.String(), "--orig-port-dst", "443").CombinedOutput()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 && strings.HasSuffix(strings.TrimSpace(string(output)), ": 0 flow entries have been deleted.") {
		return nil
	}
	if err != nil {
		return fmt.Errorf("revoke HTTPS conntrack entries: %w: %s", err, output)
	}
	return nil
}

func (Busctl) Guard(ctx context.Context) error {
	for name, want := range map[string]string{"CleanupOnExit": "no", "StrictForwardPorts": "yes", "FirewallBackend": "nftables", "NftablesFlowtable": "off", "NftablesTableOwner": "yes"} {
		raw, err := busCall(ctx, firewalldPath+"/config", "org.freedesktop.DBus.Properties", "Get", "ss", firewalldBus+".config", name)
		if err != nil {
			return err
		}
		var value struct {
			Data string `json:"data"`
		}
		if err = json.Unmarshal(raw, &value); err != nil || value.Data != want {
			return fmt.Errorf("firewalld %s must be %s; activate the NixOS egress bootstrap", name, want)
		}
	}
	return nil
}

func busCall(ctx context.Context, path, iface, method string, args ...string) (json.RawMessage, error) {
	cmd := []string{"--system", "--allow-interactive-authorization=no", "--json=short", "call", firewalldBus, path, iface, method}
	output, err := exec.CommandContext(ctx, "busctl", append(cmd, args...)...).Output()
	if err != nil {
		return nil, fmt.Errorf("firewalld %s: %w", method, err)
	}
	var result struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, err
	}
	if len(result.Data) != 1 {
		return nil, fmt.Errorf("unexpected firewalld %s reply", method)
	}
	return result.Data[0], nil
}

func (Busctl) Owner(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "busctl", "--system", "--allow-interactive-authorization=no", "--json=short", "call", "org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "GetNameOwner", "s", firewalldBus).Output()
	if err != nil {
		return "", err
	}
	var result struct {
		Data []string `json:"data"`
	}
	err = json.Unmarshal(out, &result)
	if err != nil || len(result.Data) != 1 {
		return "", fmt.Errorf("cannot identify firewalld owner")
	}
	return result.Data[0], nil
}

func (Busctl) Read(ctx context.Context, kind, name string, permanent bool) (Settings, error) {
	path, iface, method := firewalldPath, firewalldBus+"."+kind, "getPolicySettings"
	if kind == "zone" {
		method = "getZoneSettings2"
	}
	args := []string{"s", name}
	if permanent {
		lookup := "getPolicyByName"
		if kind == "zone" {
			lookup = "getZoneByName"
		}
		raw, err := busCall(ctx, firewalldPath+"/config", firewalldBus+".config", lookup, "s", name)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &path); err != nil {
			return nil, err
		}
		iface, method, args = firewalldBus+".config."+kind, "getSettings", nil
		if kind == "zone" {
			method = "getSettings2"
		}
	}
	raw, err := busCall(ctx, path, iface, method, args...)
	if err != nil {
		return nil, err
	}
	var values map[string]struct {
		Data any `json:"data"`
	}
	if err = json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	settings := Settings{}
	for key, value := range values {
		settings[key] = value.Data
	}
	return settings, nil
}

func (Busctl) Write(ctx context.Context, name, key string, values []string) error {
	args := []string{"--system", "--allow-interactive-authorization=no", "call", firewalldBus, firewalldPath, firewalldBus + ".policy", "setPolicySettings", "sa{sv}", name, "1", key, "as", strconv.Itoa(len(values))}
	output, err := exec.CommandContext(ctx, "busctl", append(args, values...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("firewalld runtime %s %s: %w: %s", name, key, err, output)
	}
	return nil
}
