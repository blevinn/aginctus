package testenv

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeVMBackend struct{ name BackendName }

func (f fakeVMBackend) Name() BackendName { return f.name }
func (f fakeVMBackend) Capabilities() BackendCapabilities { return BackendCapabilities{} }
func (f fakeVMBackend) Create(context.Context, VMCreateSpec) (VMRef, error) {
	return VMRef{}, ErrUnsupportedAction
}
func (f fakeVMBackend) Status(context.Context, VMRef) (VMStatus, error) {
	return VMStatus{}, ErrUnsupportedAction
}
func (f fakeVMBackend) Exec(context.Context, VMRef, []string, time.Duration) (VMExecResult, error) {
	return VMExecResult{}, ErrUnsupportedAction
}
func (f fakeVMBackend) Snapshot(context.Context, VMRef, string) error { return ErrUnsupportedAction }
func (f fakeVMBackend) Reset(context.Context, VMRef, string) error { return ErrUnsupportedAction }
func (f fakeVMBackend) Destroy(context.Context, VMRef) error { return ErrUnsupportedAction }
func (f fakeVMBackend) ListOwned(context.Context) ([]VMStatus, error) {
	return nil, ErrUnsupportedAction
}

func TestBackendCatalogIsOfflineIncusOnly(t *testing.T) {
	all := SupportedVMBackends()
	if len(all) != 1 || all[0].Name != IncusBackend ||
		all[0].Lifecycle != "planned" || all[0].AgentAccessible {
		t.Fatalf("catalog advertises unsafe runtime: %#v", all)
	}
	all[0].Project = "hijacked"
	desc, err := VMBackendDescriptor("incus")
	if err != nil || desc.Project != DefaultVMProject {
		t.Fatalf("catalog mutated: %#v %v", desc, err)
	}
	for _, name := range []string{"", "qemu", "ssh", "incus://root", "incus;sh", "Incus"} {
		if _, err := VMBackendDescriptor(name); !errors.Is(err, ErrUnsupportedBackend) {
			t.Errorf("accepted backend %q: %v", name, err)
		}
	}
}

func TestVMCreateSpecRejectsUntrustedPolicies(t *testing.T) {
	good := VMCreateSpec{
		Backend: IncusBackend, Project: DefaultVMProject,
		ImageFingerprint: strings.Repeat("a", 64),
		CPU: 2, MemoryMiB: 4096, RootDiskGiB: 16,
		TimeToLive: 30 * time.Minute,
	}
	if err := good.Validate(); err != nil { t.Fatal(err) }
	for _, mutate := range []func(*VMCreateSpec){
		func(s *VMCreateSpec) { s.Backend = "qemu" },
		func(s *VMCreateSpec) { s.Project = "default" },
		func(s *VMCreateSpec) { s.ImageFingerprint = "images:nixos/latest" },
		func(s *VMCreateSpec) { s.ImageFingerprint = strings.Repeat("A", 64) },
		func(s *VMCreateSpec) { s.CPU = 3 },
		func(s *VMCreateSpec) { s.MemoryMiB = 8192 },
		func(s *VMCreateSpec) { s.RootDiskGiB = 0 },
		func(s *VMCreateSpec) { s.TimeToLive = 3 * time.Hour },
	} {
		spec := good
		mutate(&spec)
		if err := spec.Validate(); err == nil {
			t.Errorf("accepted invalid VM spec: %#v", spec)
		}
	}
}

func TestVMRefNeverConfersOwnership(t *testing.T) {
	ref := VMRef{
		Backend: IncusBackend, Project: DefaultVMProject,
		Instance: "testenv-vm-123",
		RunID: "vm-" + strings.Repeat("a", 24),
		ImageFingerprint: strings.Repeat("b", 64),
	}
	if err := ref.Validate(); err != nil { t.Fatal(err) }
	for _, mutate := range []func(*VMRef){
		func(r *VMRef) { r.Backend = "qemu" },
		func(r *VMRef) { r.Project = "default" },
		func(r *VMRef) { r.Instance = "../../pilot" },
		func(r *VMRef) { r.RunID = "vm-unknown" },
		func(r *VMRef) { r.ImageFingerprint = "mutable:latest" },
	} {
		bad := ref
		mutate(&bad)
		if err := bad.Validate(); err == nil {
			t.Errorf("accepted invalid resource handle %#v", bad)
		}
	}
}

func TestBackendRegistryIsExplicitlyInjected(t *testing.T) {
	empty, err := NewBackendRegistry()
	if err != nil { t.Fatal(err) }
	if _, err := empty.Resolve("incus"); !errors.Is(err, ErrBackendUnavailable) {
		t.Fatalf("uninstalled backend active: %v", err)
	}
	reg, err := NewBackendRegistry(fakeVMBackend{name: IncusBackend})
	if err != nil { t.Fatal(err) }
	backend, err := reg.Resolve("incus")
	if err != nil || backend.Name() != IncusBackend {
		t.Fatalf("backend resolution failed: %v", err)
	}
	if err := backend.Destroy(context.Background(), VMRef{}); !errors.Is(err, ErrUnsupportedAction) {
		t.Fatalf("backend placeholder mutated: %v", err)
	}
	if _, err := reg.Resolve("qemu"); !errors.Is(err, ErrUnsupportedBackend) {
		t.Fatalf("unplanned backend resolved: %v", err)
	}
	if _, err := NewBackendRegistry(fakeVMBackend{name: IncusBackend}, fakeVMBackend{name: IncusBackend}); err == nil {
		t.Fatal("accepted duplicate backend")
	}
	if _, err := NewBackendRegistry(fakeVMBackend{name: "qemu"}); err == nil {
		t.Fatal("accepted unsupported backend")
	}
	if _, err := NewBackendRegistry(nil); err == nil { t.Fatal("accepted nil backend") }
}
