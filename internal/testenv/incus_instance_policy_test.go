package testenv

import (
	"context"
	"errors"
	"testing"
	"time"
)

func instancePolicyDrifts() map[string]func(*IncusVMInstance) {
	drifts := map[string]func(*IncusVMInstance){}
	for _, key := range []string{
		"raw.qemu", "raw.qemu.conf", "security.secureboot", "security.privileged",
		"limits.memory.hotplug", "boot.autostart", "snapshots.schedule",
		"cloud-init.user-data", "user.user-data", "user.vendor-data", "user.network-config",
		"environment.INCUS_SOCKET", "agent.nic_config", "future.unreviewed",
	} {
		drifts["config/"+key] = func(v *IncusVMInstance) { v.Config[key] = "synthetic-unapproved" }
		drifts["empty-config/"+key] = func(v *IncusVMInstance) { v.Config[key] = "" }
	}
	for _, key := range []string{"raw.mount.options", "recursive", "io.bus", "size.state", "source", "future.unreviewed"} {
		drifts["disk/"+key] = func(v *IncusVMInstance) { v.Devices["root"][key] = "" }
	}
	return drifts
}

func TestIncusInstancePolicyDriftBlocksLifecycleAndRecovery(t *testing.T) {
	ctx := context.Background()
	for name, drift := range instancePolicyDrifts() {
		t.Run(name, func(t *testing.T) {
			client := &fakeGuestIncusClient{fakeIncusVMClient: newFakeIncusClient(t)}
			backend, err := newTestIncusBackend(client, "testenv")
			if err != nil {
				t.Fatal(err)
			}
			ref, err := backend.Create(ctx, admittedSpec())
			if err != nil {
				t.Fatal(err)
			}
			vm := client.vms[ref.Instance]
			drift(&vm)
			client.vms[ref.Instance] = vm
			client.calls = nil
			if _, err := backend.Status(ctx, ref); !errors.Is(err, ErrOwnershipMismatch) {
				t.Fatalf("status accepted drift: %v", err)
			}
			if _, err := backend.Exec(ctx, ref, []string{"/bin/true"}, time.Second); !errors.Is(err, ErrOwnershipMismatch) {
				t.Fatalf("exec accepted drift: %v", err)
			}
			if err := backend.Destroy(ctx, ref); !errors.Is(err, ErrOwnershipMismatch) {
				t.Fatalf("destroy accepted drift: %v", err)
			}
			result, err := ReconcileExpiredIncusVMs(ctx, backend, ref.Policy.ExpiresAt)
			if !errors.Is(err, ErrOwnershipMismatch) || result.Destroyed != 0 {
				t.Fatalf("recovery accepted drift: %+v %v", result, err)
			}
			if len(client.argv) != 0 || len(client.vms) != 1 || len(client.calls) != 1 || client.calls[0] != "list" {
				t.Fatalf("drift caused guest execution or mutation: %v", client.calls)
			}
		})
	}
}

func TestIncusInstancePolicyDriftBlocksInitialStart(t *testing.T) {
	for name, drift := range instancePolicyDrifts() {
		t.Run(name, func(t *testing.T) {
			client := &faultIncusClient{fakeIncusVMClient: newFakeIncusClient(t)}
			client.afterCreate = func(plan IncusInstancePlan) {
				vm := client.vms[plan.Name]
				drift(&vm)
				client.vms[plan.Name] = vm
			}
			backend, err := newTestIncusBackend(client, "testenv")
			if err != nil {
				t.Fatal(err)
			}
			ref, err := backend.Create(context.Background(), admittedSpec())
			if !errors.Is(err, ErrOwnershipMismatch) || ref.RunID == "" {
				t.Fatalf("lost recoverable handle or accepted drift: %+v %v", ref, err)
			}
			if len(client.calls) != 1 || client.calls[0] != "create" || client.vms[ref.Instance].Status != "Stopped" {
				t.Fatalf("unapproved VM started: %v", client.calls)
			}
		})
	}
}

func TestIncusInstancePolicyAllowsDaemonMetadata(t *testing.T) {
	client := newFakeIncusClient(t)
	backend, err := newTestIncusBackend(client, "testenv")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ref, err := backend.Create(ctx, admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	vm := client.vms[ref.Instance]
	vm.Config["volatile.base_image"] = ref.ImageFingerprint
	vm.Config["volatile.uuid"] = "synthetic-daemon-uuid"
	vm.Config["image.os"] = "NixOS"
	client.vms[ref.Instance] = vm
	if _, err := backend.Status(ctx, ref); err != nil {
		t.Fatal(err)
	}
	result, err := ReconcileExpiredIncusVMs(ctx, backend, ref.Policy.ExpiresAt)
	if err != nil || result.Destroyed != 1 || len(client.vms) != 0 {
		t.Fatalf("daemon metadata blocked owned recovery: %+v %v", result, err)
	}
}

func TestIncusInstancePolicyDriftAfterStopBlocksDelete(t *testing.T) {
	for name, drift := range instancePolicyDrifts() {
		t.Run(name, func(t *testing.T) {
			client := &faultIncusClient{fakeIncusVMClient: newFakeIncusClient(t)}
			backend, err := newTestIncusBackend(client, "testenv")
			if err != nil {
				t.Fatal(err)
			}
			spec := admittedSpec()
			spec.Retention = VMRetained
			ctx := context.Background()
			ref, err := backend.Create(ctx, spec)
			if err != nil {
				t.Fatal(err)
			}
			client.afterStop = func(name string) {
				vm := client.vms[name]
				drift(&vm)
				client.vms[name] = vm
			}
			client.calls = nil
			if err := backend.Destroy(ctx, ref); !errors.Is(err, ErrOwnershipMismatch) {
				t.Fatalf("delete accepted post-stop drift: %v", err)
			}
			if len(client.calls) != 1 || client.calls[0] != "stop" || len(client.vms) != 1 {
				t.Fatalf("post-stop drift caused deletion: %v", client.calls)
			}
		})
	}
}
