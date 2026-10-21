package firewall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"time"
)

const EgressZone = "aginctus-egress"
const EgressOwner = "aginctus:egress:v1; NixOS structure, Aginctus runtime grants"
const EgressHTTPS = "aginctus-egress-https"
const EgressDNS = "aginctus-egress-host"

var uplinkZoneName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// Settings contains every property returned by firewalld, including unknown
// properties. Verification rejects unknown fields, not selected text.
type Settings map[string]any

type EgressBus interface {
	Guard(context.Context) error
	Forget(context.Context, string) error
	Read(context.Context, string, string, bool) (Settings, error)
	Write(context.Context, string, string, []string) error
	Owner(context.Context) (string, error)
}

type EgressGrant struct {
	Source string
	Uplink string
	HTTPS  []string
	DNS    []string
}

// EgressState is private to the root-owned reconciler. Pending records intent
// before a non-atomic D-Bus update so interrupted writes can be safely retried.
type EgressState struct {
	Current EgressGrant
	Pending *EgressGrant
}

type EgressStore interface {
	Load() (EgressState, error)
	Save(EgressState) error
}

// EgressObjects is the ownership contract shared with nix/nixos-module.nix.
// NixOS owns all structure and empty permanent grants. Only the two grant
// policies' rich_rules and the HTTPS policy's egress_zones may change at runtime.
func EgressObjects(bridge string) map[string]Settings {
	base := func(target string) Settings { return Settings{"description": EgressOwner, "target": target} }
	zone := base("DROP")
	zone["interfaces"] = []string{bridge}
	zone["ingress_priority"], zone["egress_priority"] = float64(-32768), float64(-32768)
	objects := map[string]Settings{EgressZone: zone}
	for _, p := range []struct {
		name, ingress, egress, target string
		priority                      int
	}{
		{"aginctus-egress-out", EgressZone, "ANY", "DROP", -500},
		{"aginctus-egress-in", "ANY", EgressZone, "DROP", -500},
		{"aginctus-egress-input", EgressZone, "HOST", "DROP", -500},
		{EgressHTTPS, EgressZone, "", "CONTINUE", -1000},
		{EgressDNS, EgressZone, "HOST", "CONTINUE", -1000},
	} {
		s := base(p.target)
		s["ingress_zones"], s["egress_zones"], s["priority"] = []string{p.ingress}, []string{p.egress}, float64(p.priority)
		if p.egress == "" {
			s["egress_zones"] = []string{}
		}
		objects[p.name] = s
	}
	return objects
}

// VerifySettings allows only documented neutral defaults beyond the contract.
// A newly introduced security setting fails closed even if it appears neutral.
func VerifySettings(got, want Settings) error {
	defaults := Settings{
		"short": "", "version": "", "services": []string{}, "ports": []string{},
		"protocols": []string{}, "source_ports": []string{}, "forward_ports": []string{},
		"icmp_blocks": []string{}, "icmp_block_inversion": false, "masquerade": false,
		"forward": false, "sources": []string{}, "rules_str": []string{}, "rich_rules": []string{},
		"ingress_priority": float64(0), "egress_priority": float64(0), "disable": false,
		"helpers": []string{},
	}
	for key, value := range want {
		if !settingsEqual(got[key], value) {
			return fmt.Errorf("firewalld setting %s differs from ownership contract", key)
		}
	}
	for key, value := range got {
		if _, ok := want[key]; ok {
			continue
		}
		neutral, ok := defaults[key]
		if !ok || !settingsEqual(value, neutral) {
			return fmt.Errorf("unexpected firewalld setting %s", key)
		}
	}
	return nil
}

func settingsEqual(a, b any) bool {
	// Normalizing JSON also equates omitted empty lists and typed bus values.
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	if string(x) == string(y) {
		return true
	}
	var ax, bx []string
	if json.Unmarshal(x, &ax) == nil && json.Unmarshal(y, &bx) == nil {
		sort.Strings(ax)
		sort.Strings(bx)
		return slices.Equal(ax, bx)
	}
	return false
}

// EgressRules is the single rule generator used by planning and reconciliation.
// Routes includes all nondefault routes from every host routing table, including
// connected public networks and local host addresses. IPv6 is never granted.
func EgressRules(source, bridgeCIDR, uplink string, routes []netip.Prefix) (EgressGrant, error) {
	grant := EgressGrant{Uplink: uplink}
	ip, err := netip.ParsePrefix(source)
	bridge, bridgeErr := netip.ParsePrefix(bridgeCIDR)
	if err != nil || bridgeErr != nil || !ip.Addr().Is4() || !bridge.Addr().Is4() ||
		bridge.Bits() < 16 || bridge.Bits() > 30 || ip.Bits() != bridge.Bits() || !bridge.Masked().Contains(ip.Addr()) || !bridge.Masked().Contains(ip.Addr().Next()) || ip.Addr() == bridge.Addr() ||
		ip.Addr() == bridge.Masked().Addr() || uplink == "" || uplink == "ANY" || uplink == "HOST" ||
		uplink == "trusted" || uplink == EgressZone || !uplinkZoneName.MatchString(uplink) {
		return grant, fmt.Errorf("invalid egress source, bridge or uplink")
	}
	grant.Source = ip.Addr().String()
	blocked := []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4"}
	for _, route := range routes {
		if !route.IsValid() || !route.Addr().Is4() {
			return grant, fmt.Errorf("invalid host IPv4 route")
		}
		if route.Bits() != 0 {
			blocked = append(blocked, route.Masked().String())
		}
	}
	sort.Strings(blocked)
	blocked = slices.Compact(blocked)
	for _, dest := range blocked {
		grant.HTTPS = append(grant.HTTPS, fmt.Sprintf(`rule priority="-300" family="ipv4" source address="%s/32" destination address="%s" drop`, ip.Addr(), dest))
	}
	grant.HTTPS = append(grant.HTTPS, fmt.Sprintf(`rule priority="-200" family="ipv4" source address="%s/32" port port="443" protocol="tcp" accept`, ip.Addr()))
	grant.DNS, err = FirewalldRules(&Policy{Interface: "aginctus-egress", Address: bridge.Addr().String(), Subnet: bridge.Masked().String()})
	return grant, err
}

func grantSettings(s Settings, grant EgressGrant, name string) Settings {
	copy := Settings{}
	for k, v := range s {
		copy[k] = v
	}
	if name == EgressHTTPS {
		if grant.Uplink != "" {
			copy["egress_zones"] = []string{grant.Uplink}
		}
		copy["rich_rules"] = grant.HTTPS
	}
	if name == EgressDNS {
		copy["rich_rules"] = grant.DNS
	}
	return copy
}

func verifyRuntime(got Settings, base Settings, name string, state EgressState) error {
	if VerifySettings(got, base) == nil {
		return nil
	} // Reload/restart bootstrap.
	grants := []EgressGrant{state.Current}
	if state.Pending != nil {
		grants = append(grants, *state.Pending)
	}
	for _, grant := range grants {
		want := grantSettings(base, grant, name)
		// An interrupted setter can leave any subset of that grant's rules.
		if name == EgressHTTPS || name == EgressDNS {
			rules := want["rich_rules"]
			want["rich_rules"] = got["rich_rules"]
			matches := VerifySettings(got, want) == nil
			if name == EgressHTTPS {
				want["egress_zones"] = []string{}
				matches = matches || VerifySettings(got, want) == nil
			}
			if matches && ruleSubset(got["rich_rules"], rules) {
				return nil
			}
		}
	}
	return fmt.Errorf("runtime %s drift; refusing reconciliation", name)
}

func ruleSubset(actual, allowed any) bool {
	var a, b []string
	x, _ := json.Marshal(actual)
	y, _ := json.Marshal(allowed)
	if json.Unmarshal(x, &a) != nil || json.Unmarshal(y, &b) != nil {
		return false
	}
	for _, rule := range a {
		if !slices.Contains(b, rule) {
			return false
		}
	}
	return true
}

// ReconcileEgress must run under the host-wide lifecycle lock. Discovery runs
// again before and after grants, bounding races with Incus/network changes.
// Root administrators remain concurrent authorities: they must serialize host
// maintenance; neither Incus nor firewalld supplies a cross-daemon transaction.
func ReconcileEgress(ctx context.Context, bus EgressBus, store EgressStore, bridge string, discover func(context.Context) (EgressGrant, error)) (err error) {
	guardErr := bus.Guard(ctx)
	state, loadErr := store.Load()
	owner, ownerErr := bus.Owner(ctx)
	objects := EgressObjects(bridge)
	sources := []string{state.Current.Source}
	if state.Pending != nil {
		sources = append(sources, state.Pending.Source)
	}
	runtimes := map[string]Settings{}
	owned := map[string]bool{}
	names := make([]string, 0, len(objects))
	for name := range objects {
		names = append(names, name)
	}
	sort.Strings(names)
	// Establish each grant object's ownership independently before any early
	// exit. A foreign object is never modified, even during best-effort cleanup.
	var ownershipErr error
	for _, name := range []string{EgressHTTPS, EgressDNS} {
		runtime, e := bus.Read(ctx, "policy", name, false)
		if e != nil {
			ownershipErr = errors.Join(ownershipErr, e)
			continue
		}
		if runtime["description"] != EgressOwner {
			ownershipErr = errors.Join(ownershipErr, fmt.Errorf("runtime %s ownership collision", name))
			continue
		}
		owned[name] = true
		// Recover source-specific conntrack withdrawal even if the journal is
		// unreadable. Only exact IPv4 /32 source tokens are eligible.
		raw, _ := json.Marshal(runtime["rich_rules"])
		var rules []string
		if json.Unmarshal(raw, &rules) == nil {
			for _, rule := range rules {
				for _, match := range regexp.MustCompile(`source address="([0-9.]+)/32"`).FindAllStringSubmatch(rule, -1) {
					if ip, e := netip.ParseAddr(match[1]); e == nil && ip.Is4() {
						sources = append(sources, ip.String())
					}
				}
			}
		}
	}
	clear := func(c context.Context) error {
		var cleanupErr error
		if owned[EgressHTTPS] {
			cleanupErr = errors.Join(cleanupErr, bus.Write(c, EgressHTTPS, "egress_zones", nil), bus.Write(c, EgressHTTPS, "rich_rules", nil))
		}
		if owned[EgressDNS] {
			cleanupErr = errors.Join(cleanupErr, bus.Write(c, EgressDNS, "rich_rules", nil))
		}
		// firewalld accepts established flows before evaluating policies.
		// Withdraw only our sources' HTTPS conntrack entries, never a global flush.
		sort.Strings(sources)
		for _, source := range slices.Compact(sources) {
			if source != "" {
				cleanupErr = errors.Join(cleanupErr, bus.Forget(c, source))
			}
		}
		return cleanupErr
	}
	defer func() {
		if err != nil {
			rollback, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			if e := clear(rollback); e != nil {
				err = errors.Join(err, fmt.Errorf("runtime grant rollback incomplete: %w", e))
			}
		}
	}()
	if err = errors.Join(guardErr, loadErr, ownerErr, ownershipErr); err != nil {
		return err
	}
	for _, name := range names {
		kind := "policy"
		if name == EgressZone {
			kind = "zone"
		}
		permanent, e := bus.Read(ctx, kind, name, true)
		if e != nil {
			return fmt.Errorf("NixOS egress bootstrap missing (%s): %w", name, e)
		}
		if e = VerifySettings(permanent, objects[name]); e != nil {
			return fmt.Errorf("permanent %s: %w", name, e)
		}
		runtime, e := bus.Read(ctx, kind, name, false)
		if e != nil {
			return e
		}
		if runtime["description"] != EgressOwner {
			return fmt.Errorf("runtime %s ownership collision", name)
		}
		runtimes[name] = runtime
	}
	for _, name := range names {
		if err = verifyRuntime(runtimes[name], objects[name], name, state); err != nil {
			return err
		}
	}
	want, discoveryErr := discover(ctx)
	if discoveryErr != nil {
		if err = clear(ctx); err != nil {
			return err
		}
		return discoveryErr
	}
	if want.Uplink != "" && (len(want.HTTPS) == 0 || len(want.DNS) == 0) {
		return fmt.Errorf("incomplete runtime grant")
	}
	sources = append(sources, want.Source)
	if reflect.DeepEqual(want, state.Current) && state.Pending == nil {
		out, e := bus.Read(ctx, "policy", EgressHTTPS, false)
		if e != nil {
			return e
		}
		host, e := bus.Read(ctx, "policy", EgressDNS, false)
		if e != nil {
			return e
		}
		if VerifySettings(out, grantSettings(objects[EgressHTTPS], want, EgressHTTPS)) == nil && VerifySettings(host, grantSettings(objects[EgressDNS], want, EgressDNS)) == nil {
			return nil
		}
	}
	if err = clear(ctx); err != nil {
		return err
	}
	state.Pending = &want
	if err = store.Save(state); err != nil {
		return err
	}
	if want.Uplink != "" {
		// Populate a dormant policy. A reload resets both rules and its egress
		// binding to empty, so it cannot introduce an ANY-scoped grant race.
		if err = bus.Write(ctx, EgressHTTPS, "rich_rules", want.HTTPS); err != nil {
			return err
		}
		check, e := discover(ctx)
		if e != nil {
			return fmt.Errorf("egress discovery before grant: %w", e)
		}
		if !reflect.DeepEqual(check, want) {
			return fmt.Errorf("egress identity or routes changed before grant")
		}
		if err = bus.Write(ctx, EgressDNS, "rich_rules", want.DNS); err != nil {
			return err
		}
		// Binding the single reviewed zone activates the prepared rules in one
		// firewalld transaction. If a reload intervened, no rules are granted.
		if err = bus.Write(ctx, EgressHTTPS, "egress_zones", []string{want.Uplink}); err != nil {
			return err
		}
	}
	check, e := discover(ctx)
	if e != nil {
		return fmt.Errorf("egress discovery after grant: %w", e)
	}
	if !reflect.DeepEqual(check, want) {
		return fmt.Errorf("egress identity or routes changed after grant")
	}
	if current, e := bus.Owner(ctx); e != nil || current != owner {
		return fmt.Errorf("firewalld restarted during reconciliation")
	}
	if err = bus.Guard(ctx); err != nil {
		return err
	}
	for _, name := range names {
		kind := "policy"
		if name == EgressZone {
			kind = "zone"
		}
		got, e := bus.Read(ctx, kind, name, false)
		if e != nil {
			return e
		}
		if e = VerifySettings(got, grantSettings(objects[name], want, name)); e != nil {
			return e
		}
	}
	return store.Save(EgressState{Current: want})
}
