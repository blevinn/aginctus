package testenv

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blevinn/aginctus/internal/filestate"
)

type acceptanceFake struct {
	*fakeIncusVMClient
	missingLiveVolume bool
	lingeringVolume   bool
	badProbe          bool
	createUncertain   bool
	changeForeign     bool
	commands          [][]string
	volumeQueries     int
}

func (c *acceptanceFake) Create(ctx context.Context, p IncusInstancePlan) error {
	if err := c.fakeIncusVMClient.Create(ctx, p); err != nil {
		return err
	}
	if c.createUncertain {
		return errors.New("synthetic-private-daemon-error")
	}
	return nil
}

func (c *acceptanceFake) ExecGuest(_ context.Context, _ string, argv []string) (VMExecResult, error) {
	c.commands = append(c.commands, argv)
	if len(argv) == 1 && argv[0] == incusAgentProbeExecutable {
		if c.badProbe {
			return VMExecResult{Stdout: "synthetic-private-guest-output"}, nil
		}
		return VMExecResult{}, nil
	}
	if len(argv) != 2 || argv[0] != "/run/current-system/sw/bin/incus" || argv[1] != "--version" {
		return VMExecResult{}, errors.New("unexpected command")
	}
	return VMExecResult{Stdout: "synthetic-private-guest-version\n"}, nil
}

func (c *acceptanceFake) Stop(ctx context.Context, name string) error {
	if name == "foreign-sentinel" {
		panic("foreign mutation")
	}
	if c.changeForeign {
		v := c.vms["foreign-sentinel"]
		v.Status = "Changed"
		c.vms[v.Name] = v
	}
	return c.fakeIncusVMClient.Stop(ctx, name)
}

func (c *acceptanceFake) RootVolumePresent(_ context.Context, pool, name string) (bool, error) {
	if pool != "testenv" || !strings.HasPrefix(name, "testenv-vm-") {
		panic("unscoped volume query")
	}
	c.volumeQueries++
	_, exists := c.vms[name]
	return (exists && !c.missingLiveVolume) || c.lingeringVolume, nil
}

func acceptanceFixture(t *testing.T) (*acceptanceFake, *IncusVMBackend, IncusAcceptanceOptions) {
	t.Helper()
	c := &acceptanceFake{fakeIncusVMClient: newFakeIncusClient(t)}
	c.vms["foreign-sentinel"] = IncusVMInstance{Name: "foreign-sentinel", Type: "virtual-machine", Status: "Stopped", Config: map[string]string{"user.note": "private-foreign-metadata"}}
	b, err := newTestIncusBackend(c, "testenv")
	if err != nil {
		t.Fatal(err)
	}
	cfg := IncusAcceptanceOptions{Acknowledgement: DisposableWorkerAcknowledgement, SocketPath: "/var/lib/incus/unix.socket",
		Project: DefaultVMProject, ImageFingerprint: c.image, PolicyKeyPath: "/run/credentials/key",
		ReportRoot: "/var/lib/worker/evidence", ForeignInstance: "foreign-sentinel", Revision: "test-source"}
	return c, b, cfg
}

func runAcceptanceFixture(t *testing.T, b *IncusVMBackend, cfg IncusAcceptanceOptions, save func(IncusAcceptanceReport) error,
	wait func(context.Context, time.Time) error) (IncusAcceptanceReport, error) {
	t.Helper()
	return runIncusAcceptance(context.Background(), b, cfg, newIncusAcceptanceReport(cfg, strings.Repeat("a", 24)), save, wait)
}

func checkStatus(r IncusAcceptanceReport, id string) string {
	for _, c := range r.Results {
		if c.ID == id {
			return c.Status
		}
	}
	return "missing"
}

func TestIncusAcceptanceUsesSignedBackendAndActualScopedExpiry(t *testing.T) {
	c, b, cfg := acceptanceFixture(t)
	saves, waited := 0, false
	r, err := runAcceptanceFixture(t, b, cfg, func(r IncusAcceptanceReport) error {
		saves++
		data, err := json.Marshal(r)
		if err != nil {
			return err
		}
		for _, secret := range []string{"synthetic-private", "private-foreign-metadata"} {
			if strings.Contains(string(data), secret) {
				t.Fatal("untrusted output persisted")
			}
		}
		for _, ref := range r.Handles {
			if b.verifyRef(ref) != nil {
				t.Fatal("unsigned handle persisted")
			}
		}
		return nil
	}, func(_ context.Context, expiry time.Time) error {
		waited = true
		if time.Until(expiry) < 2*time.Minute {
			t.Fatal("TTL shortened")
		}
		b.now = func() time.Time { return expiry }
		return nil
	})
	if err != nil || r.Status != "pass" || len(r.Handles) != 2 || saves < 5 || !waited {
		t.Fatalf("bad acceptance result: %+v %v", r, err)
	}
	for _, id := range []string{"ephemeral_lifecycle", "retained_lifecycle", "scoped_ttl_recovery", "instance_cleanup", "foreign_preserved", "root_volumes"} {
		if checkStatus(r, id) != "pass" {
			t.Fatalf("missing actual check %s: %+v", id, r)
		}
	}
	for _, id := range []string{"crash_reboot", "nested_isolation", "storage_reclamation", "report_retention"} {
		if checkStatus(r, id) != "not_verified" {
			t.Fatalf("overclaimed %s", id)
		}
	}
	if len(c.vms) != 1 || c.vms["foreign-sentinel"].Status != "Stopped" || c.volumeQueries != 4 || len(c.commands) != 4 {
		t.Fatal("incorrect resources/commands/inventory", c)
	}
	if got := strings.Join(c.calls, ","); got != "list,create,start,stop,create,start,stop,delete" {
		t.Fatal("alternate lifecycle or broad sweep", got)
	}
}

func TestIncusAcceptanceFailuresKeepEvidenceAndCleanOnlyOwnedRuns(t *testing.T) {
	for _, fault := range []string{"missing-volume", "lingering-volume", "bad-probe", "uncertain-create", "foreign-change", "save-failure", "early-expiry", "cancellation"} {
		t.Run(fault, func(t *testing.T) {
			c, b, cfg := acceptanceFixture(t)
			c.missingLiveVolume = fault == "missing-volume"
			c.lingeringVolume = fault == "lingering-volume"
			c.badProbe = fault == "bad-probe"
			c.createUncertain = fault == "uncertain-create"
			c.changeForeign = fault == "foreign-change"
			var recorded IncusAcceptanceReport
			r, err := runAcceptanceFixture(t, b, cfg, func(r IncusAcceptanceReport) error {
				recorded = r
				if fault == "save-failure" {
					return errors.New("synthetic-private-storage-error")
				}
				return nil
			}, func(_ context.Context, expiry time.Time) error {
				if fault == "cancellation" {
					return context.Canceled
				}
				if fault != "early-expiry" {
					b.now = func() time.Time { return expiry }
				}
				return nil
			})
			if err == nil || r.Status != "fail" || len(r.Handles) == 0 || len(recorded.Handles) == 0 {
				t.Fatalf("failure lost evidence: %+v %v", r, err)
			}
			if len(c.vms) != 1 || checkStatus(r, "instance_cleanup") != "pass" {
				t.Fatal("owned cleanup failed", c.vms)
			}
			if fault == "lingering-volume" || fault == "missing-volume" {
				if checkStatus(r, "root_volumes") == "pass" {
					t.Fatal("instance absence substituted for volume proof")
				}
			}
			if fault == "foreign-change" && checkStatus(r, "foreign_preserved") != "fail" {
				t.Fatal("foreign drift hidden")
			}
			if (fault == "early-expiry" || fault == "cancellation") && checkStatus(r, "scoped_ttl_recovery") != "not_verified" {
				t.Fatal("TTL proof fabricated")
			}
		})
	}
}

func TestIncusAcceptancePreflightNeverAdoptsExistingRuns(t *testing.T) {
	for _, fault := range []string{"owned-run", "loose-policy", "missing-foreign", "signed-foreign", "unavailable-inventory"} {
		t.Run(fault, func(t *testing.T) {
			c, b, cfg := acceptanceFixture(t)
			switch fault {
			case "owned-run":
				if _, err := b.Create(context.Background(), admittedSpec()); err != nil {
					t.Fatal(err)
				}
			case "loose-policy":
				delete(c.policy, "restricted.devices.nic")
			case "missing-foreign":
				delete(c.vms, cfg.ForeignInstance)
			case "signed-foreign":
				c.vms[cfg.ForeignInstance].Config[incusPolicyKey] = "foreign-policy"
			case "unavailable-inventory":
				b.client = c.fakeIncusVMClient
			}
			c.calls = nil
			_, err := runAcceptanceFixture(t, b, cfg, func(IncusAcceptanceReport) error { t.Fatal("unexpected evidence update"); return nil }, waitAcceptanceExpiry)
			if err == nil {
				t.Fatal("unsafe preflight accepted")
			}
			for _, call := range c.calls {
				if call != "list" {
					t.Fatal("preflight mutated worker", call)
				}
			}
		})
	}
}

func TestIncusAcceptanceRecoveryDoesNotSweepConcurrentSignedRuns(t *testing.T) {
	c, b, cfg := acceptanceFixture(t)
	var other VMRef
	r, err := runAcceptanceFixture(t, b, cfg, func(IncusAcceptanceReport) error { return nil }, func(ctx context.Context, expiry time.Time) error {
		var err error
		other, err = b.Create(ctx, admittedSpec())
		if err != nil {
			return err
		}
		b.now = func() time.Time { return expiry }
		return nil
	})
	if err != nil || r.Status != "pass" || len(c.vms) != 2 || c.vms[other.Instance].Status != "Running" {
		t.Fatalf("scoped recovery touched another run: %+v %v", r, err)
	}
}

func TestIncusAcceptanceRejectsUntrustedOperatorConfiguration(t *testing.T) {
	_, _, cfg := acceptanceFixture(t)
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*IncusAcceptanceOptions){
		func(c *IncusAcceptanceOptions) { c.Acknowledgement = "yes" },
		func(c *IncusAcceptanceOptions) { c.SocketPath = "/tmp/pilot.socket" },
		func(c *IncusAcceptanceOptions) { c.Project = "default" },
		func(c *IncusAcceptanceOptions) { c.ImageFingerprint = "latest" },
		func(c *IncusAcceptanceOptions) { c.ForeignInstance = "testenv-vm-foreign" },
		func(c *IncusAcceptanceOptions) { c.PolicyKeyPath = "key" },
		func(c *IncusAcceptanceOptions) { c.ReportRoot = "/tmp/../evidence" },
	} {
		bad := cfg
		change(&bad)
		if _, err := RunTrustedIncusAcceptance(context.Background(), bad); err == nil {
			t.Fatal("invalid operator input reached daemon")
		}
	}
	path := filepath.Join(t.TempDir(), "marker")
	if err := os.Symlink("/dev/null", path); err != nil {
		t.Fatal(err)
	}
	if err := checkDisposableWorkerMarker(path); err == nil {
		t.Fatal("untrusted marker accepted")
	}
}

func TestIncusAcceptancePrivateEvidenceRetainsSignedHandlesAndBounds(t *testing.T) {
	_, b, cfg := acceptanceFixture(t)
	ref, err := b.Create(context.Background(), admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	r := newIncusAcceptanceReport(cfg, strings.Repeat("a", 24))
	r.Handles = append(r.Handles, ref)
	dir := t.TempDir()
	if err := saveIncusAcceptanceReport(dir, r); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "result.json")
	data, err := filestate.ReadPrivateFile(path, 32*1024)
	if err != nil {
		t.Fatal(err)
	}
	var stored IncusAcceptanceReport
	if err := json.Unmarshal(data, &stored); err != nil || len(stored.Handles) != 1 || b.verifyRef(stored.Handles[0]) != nil {
		t.Fatalf("lost authenticated recovery evidence: %v", err)
	}
	r.Revision = strings.Repeat("oversized", 5000)
	if err := saveIncusAcceptanceReport(dir, r); err == nil {
		t.Fatal("unbounded report persisted")
	}
	again, err := filestate.ReadPrivateFile(path, 32*1024)
	if err != nil || string(again) != string(data) {
		t.Fatal("failed replacement corrupted retained evidence")
	}
}
