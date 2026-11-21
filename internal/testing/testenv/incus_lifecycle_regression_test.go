package testenv

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type faultIncusClient struct {
	*fakeIncusVMClient
	fault       string
	err         error
	afterCreate func(IncusInstancePlan)
	afterStop   func(string)
}

func (c *faultIncusClient) ProjectConfig(ctx context.Context) (map[string]string, error) {
	if c.fault == "project" {
		return nil, c.err
	}
	return c.fakeIncusVMClient.ProjectConfig(ctx)
}
func (c *faultIncusClient) HasImage(ctx context.Context, image string) (bool, error) {
	if c.fault == "image" {
		return false, c.err
	}
	return c.fakeIncusVMClient.HasImage(ctx, image)
}
func (c *faultIncusClient) Get(ctx context.Context, name string) (*IncusVMInstance, error) {
	if c.fault == "get" {
		return nil, c.err
	}
	return c.fakeIncusVMClient.Get(ctx, name)
}
func (c *faultIncusClient) Create(ctx context.Context, plan IncusInstancePlan) error {
	if err := c.fakeIncusVMClient.Create(ctx, plan); err != nil {
		return err
	}
	if c.afterCreate != nil {
		c.afterCreate(plan)
	}
	if c.fault == "create" {
		return c.err
	}
	return nil
}
func (c *faultIncusClient) Start(ctx context.Context, name string) error {
	if c.fault == "start" {
		return c.err
	}
	return c.fakeIncusVMClient.Start(ctx, name)
}
func (c *faultIncusClient) Stop(ctx context.Context, name string) error {
	if c.fault == "stop" {
		if c.afterStop != nil {
			c.afterStop(name)
		}
		return c.err
	}
	err := c.fakeIncusVMClient.Stop(ctx, name)
	if c.afterStop != nil {
		c.afterStop(name)
	}
	return err
}
func (c *faultIncusClient) Delete(ctx context.Context, name string) error {
	if c.fault == "delete" {
		return c.err
	}
	return c.fakeIncusVMClient.Delete(ctx, name)
}
func (c *faultIncusClient) List(ctx context.Context) ([]IncusVMInstance, error) {
	if c.fault == "list" {
		return nil, c.err
	}
	return c.fakeIncusVMClient.List(ctx)
}

func TestIncusUncertainCreateRetainsAuthenticatedRecoveryWithoutAdoption(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(fmt.Sprint(replacement), func(t *testing.T) {
			c := &faultIncusClient{fakeIncusVMClient: newFakeIncusClient(t), fault: "create",
				err: fmt.Errorf("synthetic-secret: %w", context.DeadlineExceeded)}
			if replacement {
				c.afterCreate = func(p IncusInstancePlan) { c.vms[p.Name] = IncusVMInstance{Name: p.Name} }
			}
			b, _ := newTestIncusBackend(c, "testenv")
			ref, err := b.Create(context.Background(), admittedSpec())
			if ref.RunID == "" || !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatalf("lost safe recoverable result: %+v %v", ref, err)
			}
			c.fault = ""
			err = b.Destroy(context.Background(), ref)
			if replacement {
				if !errors.Is(err, ErrOwnershipMismatch) || len(c.vms) != 1 {
					t.Fatalf("adopted foreign replacement: %v", err)
				}
			} else if err != nil || len(c.vms) != 0 {
				t.Fatalf("owned partial create not recoverable: %v", err)
			}
		})
	}
}

func TestIncusAuthenticatedPolicyRejectsLiveDriftAndForgedHandles(t *testing.T) {
	mutations := map[string]func(*IncusVMInstance){
		"cpu":    func(v *IncusVMInstance) { v.Config["limits.cpu"] = "4" },
		"memory": func(v *IncusVMInstance) { v.Config["limits.memory"] = "10GiB" },
		"disk":   func(v *IncusVMInstance) { v.Devices["root"]["size"] = "64GiB" },
		"pool":   func(v *IncusVMInstance) { v.Devices["root"]["pool"] = "foreign" },
		"expiry": func(v *IncusVMInstance) {
			v.Config["user.aginctus.testenv.expires"] = time.Now().Add(24 * time.Hour).Format(time.RFC3339Nano)
		},
		"seal":      func(v *IncusVMInstance) { v.Config[incusPolicyMACKey] = strings.Repeat("0", 64) },
		"policy":    func(v *IncusVMInstance) { v.Config[incusPolicyKey] = `{}` },
		"retention": func(v *IncusVMInstance) { v.Ephemeral = false },
		"nesting":   func(v *IncusVMInstance) { v.Config["security.nesting"] = "false" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			c := newFakeIncusClient(t)
			b, _ := newTestIncusBackend(c, "testenv")
			ref, err := b.Create(context.Background(), admittedSpec())
			if err != nil {
				t.Fatal(err)
			}
			v := c.vms[ref.Instance]
			mutate(&v)
			c.vms[ref.Instance] = v
			calls := len(c.calls)
			if _, err := b.Status(context.Background(), ref); !errors.Is(err, ErrOwnershipMismatch) {
				t.Fatal(err)
			}
			if _, err := b.ListOwned(context.Background()); !errors.Is(err, ErrOwnershipMismatch) {
				t.Fatal(err)
			}
			if err := b.Destroy(context.Background(), ref); !errors.Is(err, ErrOwnershipMismatch) {
				t.Fatal(err)
			}
			if len(c.calls) != calls+1 || len(c.vms) != 1 {
				t.Fatal("drift caused mutation")
			}
		})
	}
	c := newFakeIncusClient(t)
	b, _ := newTestIncusBackend(c, "testenv")
	ref, err := b.Create(context.Background(), admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	ref.Policy.ExpiresAt = ref.Policy.ExpiresAt.Add(time.Hour)
	if err := b.Destroy(context.Background(), ref); !errors.Is(err, ErrOwnershipMismatch) {
		t.Fatal("forged handle admitted", err)
	}
	ref.PolicyMAC = ""
	if err := b.Destroy(context.Background(), ref); !errors.Is(err, ErrOwnershipMismatch) {
		t.Fatal("legacy unsealed handle admitted", err)
	}
}

func TestIncusStablePolicyKeySupportsRestartAndRejectsWrongKey(t *testing.T) {
	c := newFakeIncusClient(t)
	key := []byte(strings.Repeat("k", 32))
	b, err := NewIncusVMBackend(c, "testenv", key)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := b.Create(context.Background(), admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	key[0] = 'x'
	if _, err := b.Status(context.Background(), ref); err != nil {
		t.Fatal("key not copied", err)
	}
	restarted, _ := NewIncusVMBackend(c, "testenv", []byte(strings.Repeat("k", 32)))
	owned, err := restarted.ListOwned(context.Background())
	if err != nil || len(owned) != 1 || owned[0].Ref != ref {
		t.Fatalf("restart lost authenticated policy: %+v %v", owned, err)
	}
	wrong, _ := NewIncusVMBackend(c, "testenv", key)
	if _, err := wrong.ListOwned(context.Background()); !errors.Is(err, ErrOwnershipMismatch) {
		t.Fatal("wrong key adopted run", err)
	}
	if _, err := NewIncusVMBackend(c, "testenv", nil); err == nil {
		t.Fatal("missing key admitted")
	}
}

func TestIncusAbsentCleanupRequiresVerifiedProjectAndHandlesStopRace(t *testing.T) {
	c := &faultIncusClient{fakeIncusVMClient: newFakeIncusClient(t)}
	b, _ := newTestIncusBackend(c, "testenv")
	ref, err := b.Create(context.Background(), admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	delete(c.vms, ref.Instance)
	if err := b.Destroy(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	c.fault = "project"
	c.err = ErrIncusInstanceMissing
	if err := b.Destroy(context.Background(), ref); err == nil {
		t.Fatal("project failure masqueraded as instance absence")
	}
	c.fault = ""
	c.policy["restricted"] = "false"
	if err := b.Destroy(context.Background(), ref); err == nil {
		t.Fatal("unrestricted project admitted absent cleanup")
	}
	c = &faultIncusClient{fakeIncusVMClient: newFakeIncusClient(t)}
	b, _ = newTestIncusBackend(c, "testenv")
	ref, err = b.Create(context.Background(), admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	c.fault = "stop"
	c.err = ErrIncusInstanceMissing
	c.afterStop = func(name string) { delete(c.vms, name) }
	if err := b.Destroy(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
}

func TestIncusRetainedCleanupRejectsPolicyDriftAfterStop(t *testing.T) {
	c := &faultIncusClient{fakeIncusVMClient: newFakeIncusClient(t)}
	b, _ := newTestIncusBackend(c, "testenv")
	spec := admittedSpec()
	spec.Retention = VMRetained
	ref, err := b.Create(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	c.afterStop = func(name string) { vm := c.vms[name]; vm.Devices["root"]["pool"] = "foreign"; c.vms[name] = vm }
	if err := b.Destroy(context.Background(), ref); !errors.Is(err, ErrOwnershipMismatch) {
		t.Fatalf("post-stop policy drift admitted: %v", err)
	}
	if len(c.vms) != 1 || strings.Contains(strings.Join(c.calls, ","), "delete") {
		t.Fatal("drifted VM deleted")
	}
}

func TestIncusProviderErrorsAreRedactedAndKeepTypedClassification(t *testing.T) {
	for _, phase := range []string{"project", "image", "get", "create", "start", "stop", "delete", "list"} {
		for _, kind := range []error{context.Canceled, context.DeadlineExceeded, ErrIncusInstanceMissing} {
			t.Run(phase+kind.Error(), func(t *testing.T) {
				c := &faultIncusClient{fakeIncusVMClient: newFakeIncusClient(t)}
				b, _ := newTestIncusBackend(c, "testenv")
				spec := admittedSpec()
				spec.Retention = VMRetained
				ref, err := b.Create(context.Background(), spec)
				if err != nil {
					t.Fatal(err)
				}
				c.fault = phase
				c.err = fmt.Errorf("synthetic-secret: %w", kind)
				switch phase {
				case "project", "image", "create", "start":
					_, err = b.Create(context.Background(), spec)
				case "get":
					_, err = b.Status(context.Background(), ref)
				case "list":
					_, err = b.ListOwned(context.Background())
				default:
					err = b.Destroy(context.Background(), ref)
				}
				if phase == "get" && kind == ErrIncusInstanceMissing || phase == "stop" && kind == ErrIncusInstanceMissing || phase == "delete" && kind == ErrIncusInstanceMissing { // a lying missing response must not permit mutation/success
					if phase != "get" && err == nil {
						t.Fatal("unverified missing response admitted")
					}
				} else if !errors.Is(err, kind) {
					t.Fatalf("classification lost: %v", err)
				}
				if err == nil || strings.Contains(err.Error(), "synthetic-secret") {
					t.Fatalf("unsafe error: %v", err)
				}
			})
		}
	}
}
