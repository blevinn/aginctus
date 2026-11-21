package testenv

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestIncusProjectPolicyRestrictsHostResources(t *testing.T) {
	p, err := IncusProjectPolicy("testenv")
	if err != nil || p.Name != DefaultVMProject {
		t.Fatalf("policy failed: %#v %v", p, err)
	}
	for key, want := range map[string]string{
		"restricted":                           "true",
		"restricted.virtual-machines.lowlevel": "block",
		"restricted.virtual-machines.nesting":  "allow",
		"restricted.devices.nic":               "block",
		"restricted.devices.disk":              "block",
		"restricted.devices.pci":               "block",
		"restricted.devices.unix-char":         "block",
		"restricted.snapshots":                 "block",
		"restricted.storage-pools.access":      "testenv",
		"limits.instances":                     "2",
		"limits.virtual-machines":              "2",
		"limits.cpu":                           "4",
		"limits.memory":                        "10GiB",
		"limits.disk":                          "64GiB",
	} {
		if p.Config[key] != want {
			t.Errorf("unsafe project %s=%q, want %q", key, p.Config[key], want)
		}
	}
	for _, bad := range []string{"", "../default", "default/../../pilot", "a,b", "/etc/passwd", "my pool"} {
		if _, err := IncusProjectPolicy(bad); err == nil {
			t.Errorf("accepted unsafe pool %q", bad)
		}
	}
}

func TestIncusInstancePlanHasNoHostDevicesOrMutableImage(t *testing.T) {
	spec := VMCreateSpec{
		Backend: IncusBackend, Project: DefaultVMProject,
		ImageFingerprint: strings.Repeat("a", 64),
		CPU:              2, MemoryMiB: 4096, RootDiskGiB: 16,
		TimeToLive: 30 * time.Minute,
	}
	ref := VMRef{
		Backend: IncusBackend, Project: DefaultVMProject,
		Instance: "testenv-vm-abc", RunID: "vm-" + strings.Repeat("b", 24),
		ImageFingerprint: spec.ImageFingerprint,
	}
	now := time.Date(2026, 10, 24, 2, 0, 0, 0, time.UTC)
	p, err := PlanIncusVM(spec, ref, "testenv", now)
	if err != nil {
		t.Fatal(err)
	}
	if p.Type != "virtual-machine" || p.ImageFingerprint != spec.ImageFingerprint ||
		!reflect.DeepEqual(p.Profiles, []string{}) || len(p.Devices) != 1 ||
		p.Config["limits.cpu"] != "2" || p.Config["limits.memory"] != "4096MiB" ||
		p.Config["security.nesting"] != "true" ||
		p.Devices["root"]["pool"] != "testenv" ||
		p.Devices["root"]["size"] != "16GiB" ||
		p.Devices["root"]["path"] != "/" ||
		!p.Ephemeral || p.ExpiresAt != now.Add(spec.TimeToLive) {
		t.Fatalf("unexpected incus plan: %#v", p)
	}
	for _, forbidden := range []string{"raw.qemu", "raw.lxc", "volatile.", "security.privileged", "limits.memory.enforce"} {
		if _, ok := p.Config[forbidden]; ok {
			t.Fatalf("unsafe instance config %q", forbidden)
		}
	}
}

func TestEphemeralIsDefaultAndRetentionMustBeExplicit(t *testing.T) {
	spec := VMCreateSpec{
		Backend: IncusBackend, Project: DefaultVMProject,
		ImageFingerprint: strings.Repeat("a", 64),
		CPU:              1, MemoryMiB: 1024, RootDiskGiB: 8,
		TimeToLive: 5 * time.Minute,
	}
	ref := VMRef{
		Backend: IncusBackend, Project: DefaultVMProject,
		Instance:         "testenv-vm-ephemeral",
		RunID:            "vm-" + strings.Repeat("c", 24),
		ImageFingerprint: spec.ImageFingerprint,
	}
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	temporary, err := PlanIncusVM(spec, ref, "testenv", now)
	if err != nil || !temporary.Ephemeral {
		t.Fatalf("default must be ephemeral: %#v %v", temporary, err)
	}
	spec.Retention = VMEphemeral
	explicit, err := PlanIncusVM(spec, ref, "testenv", now)
	if err != nil || !explicit.Ephemeral {
		t.Fatalf("explicit ephemeral policy must match default: %#v %v", explicit, err)
	}
	spec.Retention = VMRetained
	retained, err := PlanIncusVM(spec, ref, "testenv", now)
	if err != nil || retained.Ephemeral {
		t.Fatalf("retained must be an explicit opt-in: %#v %v", retained, err)
	}
	if retained.ExpiresAt != temporary.ExpiresAt {
		t.Fatal("retained instances must not evade TTL")
	}
}

func TestIncusPlanRejectsMismatchedRunIdentityAndPool(t *testing.T) {
	spec := VMCreateSpec{
		Backend: IncusBackend, Project: DefaultVMProject,
		ImageFingerprint: strings.Repeat("a", 64),
		CPU:              1, MemoryMiB: 1024, RootDiskGiB: 8,
		TimeToLive: 5 * time.Minute,
	}
	ref := VMRef{
		Backend: IncusBackend, Project: DefaultVMProject,
		Instance: "testenv-vm-good", RunID: "vm-" + strings.Repeat("b", 24),
		ImageFingerprint: spec.ImageFingerprint,
	}
	for _, change := range []func(*VMRef){
		func(v *VMRef) { v.Project = "default" },
		func(v *VMRef) { v.ImageFingerprint = strings.Repeat("c", 64) },
		func(v *VMRef) { v.Instance = "../pilot" },
	} {
		invalid := ref
		change(&invalid)
		if _, err := PlanIncusVM(spec, invalid, "testenv", time.Now()); err == nil {
			t.Fatalf("invalid instance identity admitted: %#v", invalid)
		}
	}
	if _, err := PlanIncusVM(spec, ref, "../default", time.Now()); err == nil {
		t.Fatal("accepted arbitrary host pool")
	}
	if _, err := PlanIncusVM(spec, ref, "testenv", time.Time{}); err == nil {
		t.Fatal("accepted absent trusted timestamp")
	}
	badSpec := spec
	badSpec.Project = "default"
	if _, err := PlanIncusVM(badSpec, ref, "testenv", time.Now()); err == nil {
		t.Fatal("accepted default project")
	}
	badRef := ref
	badRef.ImageFingerprint = strings.Repeat("c", 64)
	if _, err := PlanIncusVM(spec, badRef, "testenv", time.Now()); !errors.Is(err, ErrOwnershipMismatch) {
		t.Fatalf("missing ownership failure: %v", err)
	}
}
