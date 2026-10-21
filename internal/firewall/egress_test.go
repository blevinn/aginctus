package firewall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

type memoryEgressStore struct {
	state   EgressState
	fail    bool
	loadErr error
}

func (s *memoryEgressStore) Load() (EgressState, error) { return s.state, s.loadErr }
func (s *memoryEgressStore) Save(state EgressState) error {
	if s.fail {
		return errors.New("state write failed")
	}
	s.state = state
	return nil
}

type testEgressBus struct {
	permanent, runtime map[string]Settings
	writes             int
	failAt             int
	generation         string
	forgetErr          bool
	forgot             []string
	onWrite            func(string, string)
	guardErr           error
	ownerErr           error
	readErr            string
}

func (b *testEgressBus) Guard(context.Context) error { return b.guardErr }
func (b *testEgressBus) Forget(_ context.Context, source string) error {
	b.forgot = append(b.forgot, source)
	if b.forgetErr {
		return errors.New("conntrack unavailable")
	}
	return nil
}

func cloneSettings(s Settings) Settings {
	raw, _ := json.Marshal(s)
	var result Settings
	json.Unmarshal(raw, &result)
	return result
}
func newTestEgressBus() *testEgressBus {
	b := &testEgressBus{permanent: EgressObjects("egress-test"), runtime: map[string]Settings{}, generation: "one"}
	for n, s := range b.permanent {
		b.runtime[n] = cloneSettings(s)
	}
	return b
}
func (b *testEgressBus) Read(_ context.Context, kind, name string, permanent bool) (Settings, error) {
	if b.readErr == name {
		return nil, errors.New("read failure")
	}
	if permanent {
		return cloneSettings(b.permanent[name]), nil
	}
	return cloneSettings(b.runtime[name]), nil
}
func (b *testEgressBus) Owner(context.Context) (string, error) { return b.generation, b.ownerErr }
func (b *testEgressBus) Write(_ context.Context, name, key string, values []string) error {
	b.writes++
	if b.writes == b.failAt {
		return errors.New("D-Bus interruption")
	}
	b.runtime[name][key] = append([]string{}, values...)
	if b.onWrite != nil {
		b.onWrite(name, key)
	}
	return nil
}

func TestEgressReloadAndDormantRecovery(t *testing.T) {
	for step := 1; step <= 6; step++ {
		t.Run(string(rune('A'+step)), func(t *testing.T) {
			bus := newTestEgressBus()
			store := &memoryEgressStore{}
			grant := testEgressGrant(t)
			bus.onWrite = func(string, string) {
				if bus.writes == step {
					for name, s := range bus.permanent {
						bus.runtime[name] = cloneSettings(s)
					}
				}
				zones := bus.runtime[EgressHTTPS]["egress_zones"]
				if !settingsEqual(zones, []string{}) && !settingsEqual(zones, []string{grant.Uplink}) {
					t.Fatal("reload widened HTTPS scope")
				}
			}
			_ = ReconcileEgress(context.Background(), bus, store, "egress-test", func(context.Context) (EgressGrant, error) { return grant, nil })
			bus.onWrite = nil
			if err := ReconcileEgress(context.Background(), bus, store, "egress-test", func(context.Context) (EgressGrant, error) { return grant, nil }); err != nil {
				t.Fatalf("reload recovery: %v", err)
			}
		})
	}
	bus := newTestEgressBus()
	grant := testEgressGrant(t)
	bus.runtime[EgressHTTPS]["rich_rules"] = grant.HTTPS
	store := &memoryEgressStore{state: EgressState{Pending: &grant}}
	if err := ReconcileEgress(context.Background(), bus, store, "egress-test", func(context.Context) (EgressGrant, error) { return grant, nil }); err != nil {
		t.Fatalf("dormant interrupted grant: %v", err)
	}
}

func TestEgressConntrackFailureRefusesGrant(t *testing.T) {
	bus := newTestEgressBus()
	bus.forgetErr = true
	store := &memoryEgressStore{}
	grant := testEgressGrant(t)
	err := ReconcileEgress(context.Background(), bus, store, "egress-test", func(context.Context) (EgressGrant, error) { return grant, nil })
	if err == nil || !strings.Contains(err.Error(), "rollback incomplete") || len(bus.forgot) == 0 {
		t.Fatalf("unreported conntrack failure: %v", err)
	}
	if !ruleSubset(bus.runtime[EgressHTTPS]["rich_rules"], nil) {
		t.Fatal("grant installed despite conntrack failure")
	}
}
func testEgressGrant(t *testing.T) EgressGrant {
	t.Helper()
	g, e := EgressRules("10.42.40.25/24", "10.42.40.1/24", "public", []netip.Prefix{netip.MustParsePrefix("11.12.13.0/24")})
	if e != nil {
		t.Fatal(e)
	}
	return g
}

func TestEgressLifecycle(t *testing.T) {
	ctx := context.Background()
	bus := newTestEgressBus()
	store := &memoryEgressStore{}
	grant := testEgressGrant(t)
	discover := func(context.Context) (EgressGrant, error) { return grant, nil }
	reconcile := func() {
		t.Helper()
		if e := ReconcileEgress(ctx, bus, store, "egress-test", discover); e != nil {
			t.Fatal(e)
		}
	}
	reconcile()
	writes := bus.writes
	reconcile()
	if bus.writes != writes {
		t.Fatal("idempotent reconcile changed live permissions")
	}
	// A daemon restart or reload restores the empty NixOS bootstrap.
	for n, s := range bus.permanent {
		bus.runtime[n] = cloneSettings(s)
	}
	bus.generation = "two"
	reconcile()
	if bus.writes == writes {
		t.Fatal("did not restore permissions after reload")
	}
	grant.Uplink = "external"
	reconcile() // Routine zone change needs no permanent mutation.
	if !settingsEqual(bus.runtime[EgressHTTPS]["egress_zones"], []string{"external"}) {
		t.Fatal("uplink not reconciled")
	}
	if err := ReconcileEgress(ctx, bus, store, "egress-test", func(context.Context) (EgressGrant, error) { return EgressGrant{}, errors.New("gateway removed") }); err == nil || !ruleSubset(bus.runtime[EgressHTTPS]["rich_rules"], nil) {
		t.Fatal("removed gateway retained permissions")
	}
	reconcile()
	grant = EgressGrant{}
	reconcile()
	for _, n := range []string{EgressHTTPS, EgressDNS} {
		if !ruleSubset(bus.runtime[n]["rich_rules"], nil) {
			t.Fatal("cleanup retained grants")
		}
	}
}

func TestEgressFailureAndRaceRollback(t *testing.T) {
	for fail := 1; fail <= 6; fail++ {
		t.Run(string(rune('A'+fail)), func(t *testing.T) {
			bus := newTestEgressBus()
			bus.failAt = fail
			store := &memoryEgressStore{}
			grant := testEgressGrant(t)
			err := ReconcileEgress(context.Background(), bus, store, "egress-test", func(context.Context) (EgressGrant, error) { return grant, nil })
			if err == nil {
				t.Fatal("accepted interrupted mutation")
			}
			for _, n := range []string{EgressHTTPS, EgressDNS} {
				if !ruleSubset(bus.runtime[n]["rich_rules"], nil) {
					t.Fatal("rollback retained permission")
				}
			}
			bus.failAt = 0
			if err := ReconcileEgress(context.Background(), bus, store, "egress-test", func(context.Context) (EgressGrant, error) { return grant, nil }); err != nil {
				t.Fatalf("partial-failure recovery: %v", err)
			}
		})
	}
	for _, race := range []string{"identity", "restart", "storage"} {
		t.Run(race, func(t *testing.T) {
			bus := newTestEgressBus()
			store := &memoryEgressStore{}
			grant := testEgressGrant(t)
			count := 0
			err := ReconcileEgress(context.Background(), bus, store, "egress-test", func(context.Context) (EgressGrant, error) {
				count++
				if race == "identity" && count > 1 {
					return EgressGrant{}, errors.New("Incus identity changed")
				}
				if race == "restart" && count > 1 {
					bus.generation = "two"
				}
				if race == "storage" {
					store.fail = true
				}
				return grant, nil
			})
			if err == nil {
				t.Fatal("accepted race/failure")
			}
			if !ruleSubset(bus.runtime[EgressHTTPS]["rich_rules"], nil) {
				t.Fatal("race retained HTTPS grant")
			}
		})
	}
}

func TestEgressOwnershipAndDrift(t *testing.T) {
	for _, kind := range []string{"foreign permanent", "foreign runtime", "unknown field", "helper", "icmp inversion", "port", "source", "deny drift"} {
		t.Run(kind, func(t *testing.T) {
			bus := newTestEgressBus()
			store := &memoryEgressStore{}
			grant := testEgressGrant(t)
			discover := func(context.Context) (EgressGrant, error) { return grant, nil }
			if e := ReconcileEgress(context.Background(), bus, store, "egress-test", discover); e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "foreign permanent":
				bus.permanent[EgressDNS]["description"] = "someone else"
			case "foreign runtime":
				bus.runtime[EgressDNS]["description"] = "someone else"
			case "unknown field":
				bus.runtime[EgressHTTPS]["future_bypass"] = false
			case "helper":
				bus.runtime[EgressHTTPS]["helpers"] = []string{"ftp"}
			case "icmp inversion":
				bus.runtime[EgressZone]["icmp_block_inversion"] = true
			case "port":
				bus.runtime[EgressHTTPS]["source_ports"] = []string{"443/tcp"}
			case "source":
				bus.runtime[EgressHTTPS]["sources"] = []string{"0.0.0.0/0"}
			case "deny drift":
				bus.runtime["aginctus-egress-in"]["target"] = "ACCEPT"
			}
			if e := ReconcileEgress(context.Background(), bus, store, "egress-test", discover); e == nil {
				t.Fatal("accepted security drift")
			}
			if !ruleSubset(bus.runtime[EgressHTTPS]["rich_rules"], nil) {
				t.Fatal("drift retained owned HTTPS permission")
			}
			if kind == "foreign runtime" && !settingsEqual(bus.runtime[EgressDNS]["rich_rules"], grant.DNS) {
				t.Fatal("modified foreign DNS object")
			}
		})
	}
}

func TestEgressEarlyFailureWithdrawsOwnedGrants(t *testing.T) {
	for _, failure := range []string{"guard", "journal", "owner", "permanent", "deny read", "grant read"} {
		t.Run(failure, func(t *testing.T) {
			bus := newTestEgressBus()
			store := &memoryEgressStore{}
			grant := testEgressGrant(t)
			discover := func(context.Context) (EgressGrant, error) { return grant, nil }
			if err := ReconcileEgress(context.Background(), bus, store, "egress-test", discover); err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "guard":
				bus.guardErr = errors.New("unsafe daemon setting")
			case "journal":
				store.loadErr = errors.New("corrupt journal")
			case "owner":
				bus.ownerErr = errors.New("owner unavailable")
			case "permanent":
				bus.permanent[EgressHTTPS]["target"] = "ACCEPT"
			case "deny read":
				bus.readErr = "aginctus-egress-in"
			case "grant read":
				bus.readErr = EgressDNS
			}
			if err := ReconcileEgress(context.Background(), bus, store, "egress-test", discover); err == nil {
				t.Fatal("accepted failure")
			}
			if !ruleSubset(bus.runtime[EgressHTTPS]["rich_rules"], nil) || !settingsEqual(bus.runtime[EgressHTTPS]["egress_zones"], []string{}) {
				t.Fatal("owned HTTPS permission survived failure")
			}
			if len(bus.forgot) == 0 {
				t.Fatal("conntrack withdrawal omitted")
			}
		})
	}
}

func TestEgressUnsafeDestinationsLifecycle(t *testing.T) {
	grant := testEgressGrant(t)
	for _, dest := range []string{"10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "192.0.0.0/24", "198.18.0.0/15", "11.12.13.0/24"} {
		if !strings.Contains(strings.Join(grant.HTTPS, "\n"), `destination address="`+dest+`" drop`) {
			t.Fatalf("destination not denied: %s", dest)
		}
	}
	for _, source := range []string{"10.42.40.0/24", "10.42.40.1/24", "10.42.40.255/24", "::1/24"} {
		if _, e := EgressRules(source, "10.42.40.1/24", "public", nil); e == nil {
			t.Fatalf("accepted source %s", source)
		}
	}
}

// Only the disposable NixOS test enables this. It exercises the actual native
// firewalld API; the VM driver checks independent packet behavior around it.
type reloadEgressBus struct {
	Busctl
	triggered bool
}

func (b *reloadEgressBus) Write(ctx context.Context, name, key string, values []string) error {
	if err := b.Busctl.Write(ctx, name, key, values); err != nil {
		return err
	}
	if !b.triggered && name == EgressHTTPS && key == "rich_rules" && len(values) > 0 {
		b.triggered = true
		output, err := exec.CommandContext(ctx, "firewall-cmd", "--reload").CombinedOutput()
		if err != nil {
			return fmt.Errorf("fixture reload: %w: %s", err, output)
		}
	}
	return nil
}

func TestEgressRealFirewalld(t *testing.T) {
	phase := os.Getenv("AGINCTUS_DISPOSABLE_FIREWALL_PHASE")
	if phase == "" {
		t.Skip("requires disposable firewalld VM")
	}
	if marker, err := os.ReadFile("/etc/aginctus/disposable-firewall-vm"); err != nil || string(marker) != "YES" {
		t.Fatal("real-firewalld test requires the disposable VM marker")
	}
	if phase == "zone-fallback" {
		zone, err := (Busctl{}).EffectiveZone(context.Background(), "unbound0")
		if err != nil || zone != "nixos-fw-default" {
			t.Fatalf("implicit uplink classification: %q %v", zone, err)
		}
		zone, err = (Busctl{}).EffectiveZone(context.Background(), "wan-host")
		if err != nil || zone != "public" {
			t.Fatalf("explicit uplink classification: %q %v", zone, err)
		}
		return
	}
	path := "/run/egress-test-state.json"
	store := &memoryEgressStore{}
	if raw, e := os.ReadFile(path); e == nil {
		if e = json.Unmarshal(raw, &store.state); e != nil {
			t.Fatal(e)
		}
	}
	grant, e := EgressRules("10.247.83.25/24", "10.247.83.1/24", "public", []netip.Prefix{netip.MustParsePrefix("11.12.13.0/24")})
	if e != nil {
		t.Fatal(e)
	}
	if phase == "revoke" {
		grant = EgressGrant{}
	}
	discover := func(ctx context.Context) (EgressGrant, error) {
		if phase != "routing" {
			return grant, nil
		}
		rules, err := exec.CommandContext(ctx, "ip", "-j", "-4", "rule", "show").Output()
		if err != nil {
			return EgressGrant{}, err
		}
		routes, err := exec.CommandContext(ctx, "ip", "-j", "-4", "route", "show", "table", "all").Output()
		if err != nil {
			return EgressGrant{}, err
		}
		iface, blocked, err := EgressRouting(string(rules), string(routes))
		if err != nil {
			return EgressGrant{}, err
		}
		zone, err := (Busctl{}).EffectiveZone(ctx, iface)
		if err != nil || zone != "public" {
			return EgressGrant{}, fmt.Errorf("fixture uplink zone not public: %s, %v", zone, err)
		}
		grant, err = EgressRules("10.247.83.25/24", "10.247.83.1/24", zone, blocked)
		return grant, err
	}
	if phase == "reload-race" {
		grant.HTTPS = append([]string{`rule priority="-300" family="ipv4" source address="10.247.83.25/32" destination address="11.12.14.0/24" drop`}, grant.HTTPS...)
		bus := &reloadEgressBus{}
		if err := ReconcileEgress(context.Background(), bus, store, "aginctus-egress", func(context.Context) (EgressGrant, error) { return grant, nil }); err == nil || !bus.triggered {
			t.Fatalf("reload race was not refused: %v", err)
		}
		return
	}
	if e = ReconcileEgress(context.Background(), Busctl{}, store, "aginctus-egress", discover); e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(store.state)
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(store.state.Current, grant) {
		t.Fatal("reconcile did not persist final state")
	}
}
