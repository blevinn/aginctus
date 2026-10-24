package testenv

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type reconcileHookClient struct {
	*faultIncusClient
	afterList func()
}

func (c *reconcileHookClient) List(ctx context.Context) ([]IncusVMInstance, error) {
	instances, err := c.faultIncusClient.List(ctx)
	if err != nil {
		return nil, err
	}
	// Incus enumeration is a snapshot, not aliased live metadata.
	for i := range instances {
		config := make(map[string]string)
		for k, v := range instances[i].Config {
			config[k] = v
		}
		instances[i].Config = config
		devices := make(map[string]map[string]string)
		for name, settings := range instances[i].Devices {
			copy := make(map[string]string)
			for k, v := range settings {
				copy[k] = v
			}
			devices[name] = copy
		}
		instances[i].Devices = devices
	}
	if c.afterList != nil {
		c.afterList()
	}
	return instances, nil
}

func TestIncusReconcileRejectsPostDiscoveryLifetimeGenerationAndPolicyDrift(t *testing.T) {
	for _, drift := range []string{"expiry", "signed-renewal", "generation", "project", "device", "state"} {
		t.Run(drift, func(t *testing.T) {
			c := &reconcileHookClient{faultIncusClient: &faultIncusClient{fakeIncusVMClient: newFakeIncusClient(t)}}
			b, _ := newTestIncusBackend(c, "testenv")
			now := time.Now().UTC()
			b.now = func() time.Time { return now }
			ref, err := b.Create(context.Background(), admittedSpec())
			if err != nil {
				t.Fatal(err)
			}
			c.afterList = func() {
				v := c.vms[ref.Instance]
				switch drift {
				case "expiry":
					v.Config["user.aginctus.testenv.expires"] = now.Add(2 * time.Hour).Format(time.RFC3339Nano)
				case "signed-renewal":
					updated := ref
					updated.Policy.ExpiresAt = now.Add(2 * time.Hour)
					updated.PolicyMAC = b.sealRef(updated)
					policy, _ := json.Marshal(updated.Policy)
					v.Config[incusPolicyKey] = string(policy)
					v.Config[incusPolicyMACKey] = updated.PolicyMAC
					v.Config["user.aginctus.testenv.expires"] = updated.Policy.ExpiresAt.Format(time.RFC3339Nano)
				case "generation":
					v.Config["user.aginctus.testenv.run"] = "vm-" + strings.Repeat("b", 24)
				case "project":
					c.policy["restricted"] = "false"
				case "device":
					v.Devices["root"]["pool"] = "foreign"
				case "state":
					v.Status = "Frozen"
				}
				c.vms[ref.Instance] = v
			}
			summary, err := ReconcileExpiredIncusVMs(context.Background(), b, now.Add(time.Hour))
			if err == nil || summary.Destroyed != 0 || summary.Failed != 1 || len(c.vms) != 1 {
				t.Fatalf("stale snapshot authorized deletion: %+v %v", summary, err)
			}
			if drift != "state" && !errors.Is(err, ErrOwnershipMismatch) {
				t.Fatalf("lost drift classification: %v", err)
			}
			for _, call := range c.calls {
				if call == "stop" || call == "delete" {
					t.Fatalf("drift caused mutation: %v", c.calls)
				}
			}
		})
	}
}

func TestIncusExpiryDestructionRejectsLiveUnexpiredRun(t *testing.T) {
	c := newFakeIncusClient(t)
	b, _ := newTestIncusBackend(c, "testenv")
	now := time.Now().UTC()
	b.now = func() time.Time { return now }
	ref, err := b.Create(context.Background(), admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	if err := b.destroy(context.Background(), ref, now.Add(time.Minute)); !errors.Is(err, ErrOwnershipMismatch) || len(c.vms) != 1 {
		t.Fatalf("live expiry was not checked: %v", err)
	}
	if err := b.Destroy(context.Background(), ref); err != nil {
		t.Fatal("explicit manual destruction no longer works", err)
	}
}

func TestIncusReconcileToleratesAutoDeletionAfterDiscovery(t *testing.T) {
	c := &reconcileHookClient{faultIncusClient: &faultIncusClient{fakeIncusVMClient: newFakeIncusClient(t)}}
	b, _ := newTestIncusBackend(c, "testenv")
	now := time.Now().UTC()
	b.now = func() time.Time { return now }
	ref, err := b.Create(context.Background(), admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	c.afterList = func() { delete(c.vms, ref.Instance) }
	summary, err := ReconcileExpiredIncusVMs(context.Background(), b, now.Add(time.Hour))
	if err != nil || summary.Destroyed != 1 || summary.Failed != 0 {
		t.Fatalf("automatic deletion falsely failed cleanup: %+v %v", summary, err)
	}
}

func TestIncusReconcileCancellationDuringDiscoveryAndCleanupStopsFurtherMutation(t *testing.T) {
	for _, phase := range []string{"list", "stop"} {
		t.Run(phase, func(t *testing.T) {
			c := &reconcileHookClient{faultIncusClient: &faultIncusClient{fakeIncusVMClient: newFakeIncusClient(t)}}
			b, _ := newTestIncusBackend(c, "testenv")
			now := time.Now().UTC()
			b.now = func() time.Time { return now }
			for i := 0; i < 2; i++ {
				if _, err := b.Create(context.Background(), admittedSpec()); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if phase == "list" {
				c.afterList = cancel
			} else {
				c.afterStop = func(string) { cancel() }
			}
			summary, err := ReconcileExpiredIncusVMs(ctx, b, now.Add(time.Hour))
			if !errors.Is(err, context.Canceled) || summary.Destroyed != 0 {
				t.Fatalf("cancellation falsely succeeded: %+v %v", summary, err)
			}
			stops := 0
			for _, call := range c.calls {
				if call == "stop" {
					stops++
				}
			}
			if phase == "list" && stops != 0 || phase == "stop" && stops != 1 {
				t.Fatalf("canceled sweep kept mutating: %v", c.calls)
			}
		})
	}
}

func TestIncusReconcileAcceptsExactly64VerifiedCandidates(t *testing.T) {
	c := newFakeIncusClient(t)
	b, _ := newTestIncusBackend(c, "testenv")
	now := time.Now().UTC()
	b.now = func() time.Time { return now }
	for i := 0; i < MaxIncusReconcileCandidates; i++ {
		if _, err := b.Create(context.Background(), admittedSpec()); err != nil {
			t.Fatal(err)
		}
	}
	summary, err := ReconcileExpiredIncusVMs(context.Background(), b, now.Add(time.Hour))
	if err != nil || summary.Scanned != MaxIncusReconcileCandidates || summary.Destroyed != MaxIncusReconcileCandidates || len(c.vms) != 0 {
		t.Fatalf("exact reconciliation bound failed: %+v %v", summary, err)
	}
}

func TestIncusReconcileRetainedAtExpiryAndLifetimeDriftAfterStop(t *testing.T) {
	for _, drift := range []bool{false, true} {
		c := &faultIncusClient{fakeIncusVMClient: newFakeIncusClient(t)}
		b, _ := newTestIncusBackend(c, "testenv")
		now := time.Now().UTC()
		b.now = func() time.Time { return now }
		spec := admittedSpec()
		spec.Retention = VMRetained
		ref, err := b.Create(context.Background(), spec)
		if err != nil {
			t.Fatal(err)
		}
		if drift {
			c.afterStop = func(name string) {
				v := c.vms[name]
				v.Config["user.aginctus.testenv.expires"] = now.Add(time.Hour).Format(time.RFC3339Nano)
				c.vms[name] = v
			}
		}
		summary, err := ReconcileExpiredIncusVMs(context.Background(), b, ref.Policy.ExpiresAt)
		if drift {
			if !errors.Is(err, ErrOwnershipMismatch) || summary.Failed != 1 || summary.Destroyed != 0 || len(c.vms) != 1 {
				t.Fatalf("post-stop expiry drift caused deletion: %+v %v", summary, err)
			}
		} else if err != nil || summary.Destroyed != 1 || len(c.vms) != 0 {
			t.Fatalf("retained expiry boundary not cleaned: %+v %v", summary, err)
		}
	}
}
