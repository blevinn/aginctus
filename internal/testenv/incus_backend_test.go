package testenv

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeIncusVMClient struct {
	policy   map[string]string
	image    string
	vms      map[string]IncusVMInstance
	calls    []string
	conflict bool
	fail     string
}

func newFakeIncusClient(t *testing.T) *fakeIncusVMClient {
	t.Helper()
	project, err := IncusProjectPolicy("testenv")
	if err != nil {
		t.Fatal(err)
	}
	return &fakeIncusVMClient{
		policy: project.Config, image: strings.Repeat("a", 64),
		vms: make(map[string]IncusVMInstance),
	}
}
func (c *fakeIncusVMClient) ProjectConfig(context.Context) (map[string]string, error) {
	if c.fail == "policy" {
		return nil, errors.New("policy denied")
	}
	return c.policy, nil
}
func (c *fakeIncusVMClient) HasImage(_ context.Context, name string) (bool, error) {
	return name == c.image, nil
}
func (c *fakeIncusVMClient) Get(_ context.Context, name string) (*IncusVMInstance, error) {
	if c.fail == "get" {
		return nil, errors.New("permission denied")
	}
	if c.conflict {
		return &IncusVMInstance{Name: name}, nil
	}
	instance, ok := c.vms[name]
	if !ok {
		return nil, ErrIncusInstanceMissing
	}
	return &instance, nil
}
func (c *fakeIncusVMClient) Create(_ context.Context, plan IncusInstancePlan) error {
	c.calls = append(c.calls, "create")
	if c.fail == "create" {
		return errors.New("bad instance")
	}
	if _, exists := c.vms[plan.Name]; exists {
		return errors.New("collision")
	}
	c.vms[plan.Name] = IncusVMInstance{
		Name: plan.Name, Type: plan.Type, Status: "Stopped",
		Ephemeral: plan.Ephemeral, Profiles: plan.Profiles,
		Config: plan.Config, Devices: plan.Devices,
	}
	return nil
}
func (c *fakeIncusVMClient) Start(_ context.Context, name string) error {
	c.calls = append(c.calls, "start")
	if c.fail == "start" {
		return errors.New("cannot start")
	}
	vm := c.vms[name]
	vm.Status = "Running"
	c.vms[name] = vm
	return nil
}
func (c *fakeIncusVMClient) Stop(_ context.Context, name string) error {
	c.calls = append(c.calls, "stop")
	if c.fail == "stop" {
		return errors.New("cannot stop")
	}
	vm := c.vms[name]
	if vm.Ephemeral {
		delete(c.vms, name)
	} else {
		vm.Status = "Stopped"
		c.vms[name] = vm
	}
	return nil
}
func (c *fakeIncusVMClient) Delete(_ context.Context, name string) error {
	c.calls = append(c.calls, "delete")
	if c.fail == "delete" {
		return errors.New("cannot delete")
	}
	delete(c.vms, name)
	return nil
}
func (c *fakeIncusVMClient) List(context.Context) ([]IncusVMInstance, error) {
	c.calls = append(c.calls, "list")
	out := make([]IncusVMInstance, 0, len(c.vms))
	for _, vm := range c.vms {
		out = append(out, vm)
	}
	return out, nil
}
func admittedSpec() VMCreateSpec {
	return VMCreateSpec{
		Backend: IncusBackend, Project: DefaultVMProject,
		ImageFingerprint: strings.Repeat("a", 64),
		CPU:              2, MemoryMiB: 4096, RootDiskGiB: 16,
		TimeToLive: 30 * time.Minute,
	}
}

func newTestIncusBackend(client IncusVMClient, pool string) (*IncusVMBackend, error) {
	return NewIncusVMBackend(client, pool, []byte(strings.Repeat("synthetic-test-key", 2)[:32]))
}

func TestIncusCreateStatusDestroyEphemeral(t *testing.T) {
	ctx := context.Background()
	client := newFakeIncusClient(t)
	backend, err := newTestIncusBackend(client, "testenv")
	if err != nil {
		t.Fatal(err)
	}
	backend.now = func() time.Time { return time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC) }
	ref, err := backend.Create(ctx, admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	if err := ref.Validate(); err != nil {
		t.Fatal(err)
	}
	if ref.Instance != "testenv-"+ref.RunID {
		t.Fatalf("name not owned: %#v", ref)
	}
	vm := client.vms[ref.Instance]
	if !vm.Ephemeral || vm.Status != "Running" || len(vm.Devices) != 1 ||
		len(vm.Profiles) != 0 || vm.Devices["root"]["pool"] != "testenv" {
		t.Fatalf("unexpected VM state: %#v", vm)
	}
	status, err := backend.Status(ctx, ref)
	if err != nil || status.State != "Running" ||
		!status.ExpiresAt.Equal(backend.now().Add(admittedSpec().TimeToLive)) {
		t.Fatalf("status not verified: %#v %v", status, err)
	}
	owned, err := backend.ListOwned(ctx)
	if err != nil || len(owned) != 1 || owned[0].Ref.RunID != ref.RunID {
		t.Fatalf("incorrect owned list: %#v %v", owned, err)
	}
	if err := backend.Destroy(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, ok := client.vms[ref.Instance]; ok {
		t.Fatal("ephemeral still present after stop")
	}
	if len(client.calls) != 4 {
		t.Fatalf("unexpected calls: %v", client.calls)
	}
	for _, call := range client.calls {
		if call == "delete" {
			t.Fatal("ephemeral cleanup should auto-delete on stop")
		}
	}
}

func TestIncusRetainedRequiresStopAndDelete(t *testing.T) {
	ctx := context.Background()
	client := newFakeIncusClient(t)
	backend, err := newTestIncusBackend(client, "testenv")
	if err != nil {
		t.Fatal(err)
	}
	spec := admittedSpec()
	spec.Retention = VMRetained
	ref, err := backend.Create(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if client.vms[ref.Instance].Ephemeral {
		t.Fatal("retained VM marked ephemeral")
	}
	if err := backend.Destroy(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, ok := client.vms[ref.Instance]; ok {
		t.Fatal("retained VM not deleted")
	}
	if got := strings.Join(client.calls, ","); !strings.Contains(got, "stop,delete") {
		t.Fatalf("retained VM did not follow stop+delete path: %v", client.calls)
	}
}

func TestIncusRejectsForeignInstanceBeforeStop(t *testing.T) {
	ctx := context.Background()
	client := newFakeIncusClient(t)
	backend, _ := newTestIncusBackend(client, "testenv")
	ref, err := backend.Create(ctx, admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	vm := client.vms[ref.Instance]
	vm.Config["user.aginctus.testenv.run"] = "vm-" + strings.Repeat("b", 24)
	client.vms[ref.Instance] = vm
	calls := len(client.calls)
	if err := backend.Destroy(ctx, ref); !errors.Is(err, ErrOwnershipMismatch) {
		t.Fatalf("foreign instance not denied: %v", err)
	}
	if calls != len(client.calls) {
		t.Fatalf("unauthorized stop attempted: %v", client.calls)
	}
	if _, ok := client.vms[ref.Instance]; !ok {
		t.Fatal("foreign VM deleted")
	}
}

func TestIncusProjectPolicyDriftAndNameCollisionFailClosed(t *testing.T) {
	ctx := context.Background()
	client := newFakeIncusClient(t)
	backend, _ := newTestIncusBackend(client, "testenv")
	delete(client.policy, "restricted.devices.nic")
	if _, err := backend.Create(ctx, admittedSpec()); err == nil {
		t.Fatal("unrestricted project was accepted")
	}
	if len(client.calls) != 0 {
		t.Fatal("project mismatch invoked mutation")
	}
	client = newFakeIncusClient(t)
	backend, _ = newTestIncusBackend(client, "testenv")
	client.conflict = true
	if _, err := backend.Create(ctx, admittedSpec()); err == nil {
		t.Fatal("name collision caused adoption")
	}
	if len(client.calls) != 0 {
		t.Fatal("collision invoked mutation")
	}
	client = newFakeIncusClient(t)
	backend, _ = newTestIncusBackend(client, "testenv")
	client.fail = "get"
	if _, err := backend.Create(ctx, admittedSpec()); err == nil {
		t.Fatal("unverified name allowed create")
	}
	if len(client.calls) != 0 {
		t.Fatal("get failure invoked mutation")
	}
}

func TestIncusFailClosedAfterFailedStartAndUnverifiedCleanup(t *testing.T) {
	ctx := context.Background()
	client := newFakeIncusClient(t)
	backend, _ := newTestIncusBackend(client, "testenv")
	client.fail = "start"
	ref, err := backend.Create(ctx, admittedSpec())
	if err == nil || ref.RunID == "" {
		t.Fatalf("lost recoverable run identity: %#v %v", ref, err)
	}
	if _, ok := client.vms[ref.Instance]; !ok {
		t.Fatal("failed start removed recoverable instance")
	}
	// A failed start leaves a stopped VM, so cleanup must exercise Delete.
	client.fail = "delete"
	if err := backend.Destroy(ctx, ref); err == nil {
		t.Fatal("unverified cleanup was reported successful")
	}
	if _, ok := client.vms[ref.Instance]; !ok {
		t.Fatal("failed cleanup damaged instance")
	}
}

func TestIncusOwnedListIgnoresForeignButRejectsSpoofedOwnedVM(t *testing.T) {
	ctx := context.Background()
	client := newFakeIncusClient(t)
	backend, _ := newTestIncusBackend(client, "testenv")
	ref, err := backend.Create(ctx, admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	client.vms["pilot-instance"] = IncusVMInstance{Name: "pilot-instance", Type: "virtual-machine"}
	owned, err := backend.ListOwned(ctx)
	if err != nil || len(owned) != 1 {
		t.Fatalf("adopted foreign instance: %#v %v", owned, err)
	}
	vm := client.vms[ref.Instance]
	vm.Devices["extraneous"] = map[string]string{"type": "nic"}
	client.vms[ref.Instance] = vm
	if _, err := backend.ListOwned(ctx); !errors.Is(err, ErrOwnershipMismatch) {
		t.Fatalf("device injection not flagged: %v", err)
	}
}

func TestIncusCapabilitiesDoNotAdvertiseUnsafeMutations(t *testing.T) {
	backend, err := newTestIncusBackend(newFakeIncusClient(t), "testenv")
	if err != nil {
		t.Fatal(err)
	}
	caps := backend.Capabilities()
	if caps.Exec || caps.Snapshot || caps.Reset {
		t.Fatalf("premature capabilities: %#v", caps)
	}
	if _, err := backend.Exec(context.Background(), VMRef{}, []string{"/bin/sh"}, time.Second); !errors.Is(err, ErrUnsupportedAction) {
		t.Fatalf("guest exec unexpectedly admitted: %v", err)
	}
	if err := backend.Snapshot(context.Background(), VMRef{}, "snap"); !errors.Is(err, ErrUnsupportedAction) {
		t.Fatalf("snapshot unexpectedly admitted: %v", err)
	}
	if err := backend.Reset(context.Background(), VMRef{}, "snap"); !errors.Is(err, ErrUnsupportedAction) {
		t.Fatalf("reset unexpectedly admitted: %v", err)
	}
}
