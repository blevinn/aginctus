package gateway

import (
	"context"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/statevolume"
	"github.com/lxc/incus/v7/shared/api"
)

func attachmentFixture() CredentialAttachment {
	return CredentialAttachment{GatewayID: "local", ComposeProject: "aginctus-gateway", Pool: "default"}
}
func ownedInstance() *api.Instance {
	return &api.Instance{
		Name: liteLLMServiceInstance,
		InstancePut: api.InstancePut{
			Config: map[string]string{
				"user.aginctus.managed":    "true",
				"user.aginctus.resource":   "compose-instance",
				"user.aginctus.deployment": "aginctus-gateway",
			},
			Devices: map[string]map[string]string{},
		},
		ExpandedDevices: map[string]map[string]string{
			"root": {"type": "disk", "path": "/", "pool": "default"},
		},
	}
}
func TestPrivateMountPlacement(t *testing.T) {
	a := attachmentFixture()
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	if a.volume().Name() != "aginctus-gateway-local-credentials" || a.volume().Project != "aginctus-gateway" {
		t.Fatal(a)
	}
	device := a.device()
	if device["type"] != "disk" || device["pool"] != "default" || device["path"] != "/var/lib/aginctus/chatgpt" {
		t.Fatal(device)
	}
	inst := ownedInstance()
	needed, err := plannedCredentialDevice(inst, a)
	if err != nil || !needed {
		t.Fatalf("%v %v", needed, err)
	}
	inst.ExpandedDevices[credentialDevice] = device
	needed, err = plannedCredentialDevice(inst, a)
	if err != nil || needed {
		t.Fatalf("expected idempotence, needed=%v err=%v", needed, err)
	}
}

func TestCredentialAttachmentRequiresOwnedComposeProject(t *testing.T) {
	a := attachmentFixture()
	cases := []struct {
		name string
		edit func(*api.Project) *api.Project
		want string
	}{
		{"valid owned isolated project", func(p *api.Project) *api.Project { return p }, ""},
		{"missing project", func(*api.Project) *api.Project { return nil }, "ownership"},
		{"wrong project name", func(p *api.Project) *api.Project { p.Name = "other"; return p }, "ownership"},
		{"missing managed marker", func(p *api.Project) *api.Project { delete(p.Config, "user.aginctus.managed"); return p }, "ownership"},
		{"false managed marker", func(p *api.Project) *api.Project { p.Config["user.aginctus.managed"] = "false"; return p }, "ownership"},
		{"wrong resource marker", func(p *api.Project) *api.Project { p.Config["user.aginctus.resource"] = "infrastructure"; return p }, "ownership"},
		{"missing deployment marker", func(p *api.Project) *api.Project { delete(p.Config, "user.aginctus.deployment"); return p }, "ownership"},
		{"wrong deployment marker", func(p *api.Project) *api.Project { p.Config["user.aginctus.deployment"] = "other"; return p }, "ownership"},
		{"missing isolation", func(p *api.Project) *api.Project { delete(p.Config, "features.storage.volumes"); return p }, "isolate"},
		{"disabled isolation", func(p *api.Project) *api.Project { p.Config["features.storage.volumes"] = "false"; return p }, "isolate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			project := tc.edit(ownedAuditProject())
			err := verifyCredentialComposeProject(project, a.ComposeProject)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("valid project rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q rejection, got %v", tc.want, err)
			}
		})
	}
}

func TestAttachmentFailsClosedOnOwnershipAndCollisions(t *testing.T) {
	a := attachmentFixture()
	inst := ownedInstance()
	inst.Config["user.aginctus.resource"] = "workload"
	if _, err := plannedCredentialDevice(inst, a); err == nil {
		t.Fatal("accepted unowned instance")
	}
	inst = ownedInstance()
	inst.ExpandedDevices["foreign"] = map[string]string{"type": "disk", "path": "/var/lib/aginctus", "source": "wrong"}
	if _, err := plannedCredentialDevice(inst, a); err == nil {
		t.Fatal("accepted overlapping mount")
	}
	inst = ownedInstance()
	inst.ExpandedDevices[credentialDevice] = map[string]string{"type": "disk", "path": credentialMountPath, "source": "foreign"}
	if _, err := plannedCredentialDevice(inst, a); err == nil {
		t.Fatal("accepted modified credential disk")
	}
	inst = ownedInstance()
	inst.ExpandedDevices["duplicate"] = map[string]string{"type": "disk", "path": "/other", "pool": a.Pool, "source": a.volume().Name()}
	if _, err := plannedCredentialDevice(inst, a); err == nil {
		t.Fatal("accepted duplicate private volume mount")
	}
}
func TestAttachmentRejectsForeignConsumers(t *testing.T) {
	good := "/1.0/instances/litellm-1?project=aginctus-gateway"
	for _, tc := range []struct {
		used  []string
		valid bool
	}{
		{nil, true},
		{[]string{good}, true},
		{[]string{good, "/1.0/instances/postgres-1?project=aginctus-gateway"}, false},
		{[]string{"/1.0/instances/litellm-1?project=default"}, false},
		{[]string{"/1.0/profiles/default?project=aginctus-gateway"}, false},
	} {
		err := volumeExclusivity(&api.StorageVolume{StorageVolumePut: api.StorageVolumePut{}, UsedBy: tc.used}, "aginctus-gateway")
		if (err == nil) != tc.valid {
			t.Fatalf("used=%v err=%v", tc.used, err)
		}
	}
}
func TestAttachmentRejectsUnsafeProject(t *testing.T) {
	a := attachmentFixture()
	for _, project := range []string{"default", "../escape", "", "a/b"} {
		a.ComposeProject = project
		if err := a.Validate(); err == nil {
			t.Fatalf("accepted project %q", project)
		}
	}
	a = attachmentFixture()
	if strings.Contains(a.device()["source"], "oauth") {
		t.Fatal("OAuth-specific mount")
	}
}

// A1: ensure must enforce the very same project ownership/isolation gate as
// attach and audit, *before* calling the state-volume mutation path.
func TestCredentialEnsureProjectGuardBeforeVolumeMutation(t *testing.T) {
	a := attachmentFixture()
	cases := []struct {
		name    string
		project *api.Project
		reject  bool
	}{
		{"owned isolated", ownedAuditProject(), false},
		{"absent", nil, true},
		{"wrong name", func() *api.Project { p := ownedAuditProject(); p.Name = "foreign"; return p }(), true},
		{"unmanaged", func() *api.Project { p := ownedAuditProject(); delete(p.Config, "user.aginctus.managed"); return p }(), true},
		{"foreign resource", func() *api.Project { p := ownedAuditProject(); p.Config["user.aginctus.resource"] = "foreign"; return p }(), true},
		{"wrong deployment", func() *api.Project { p := ownedAuditProject(); p.Config["user.aginctus.deployment"] = "other"; return p }(), true},
		{"shared volumes", func() *api.Project { p := ownedAuditProject(); p.Config["features.storage.volumes"] = "false"; return p }(), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			ensure := func(_ context.Context, v statevolume.GatewayCredentials) error {
				calls++
				if v.Project != a.ComposeProject || v.Pool != a.Pool || v.GatewayID != a.GatewayID {
					t.Fatalf("wrong volume target: %+v", v)
				}
				return nil
			}
			err := ensureCredentialVolume(context.Background(), a, tc.project, ensure)
			if tc.reject {
				if err == nil || calls != 0 {
					t.Fatalf("rejected project reached volume mutation: calls=%d err=%v", calls, err)
				}
			} else if err != nil || calls != 1 {
				t.Fatalf("owned project ensure: calls=%d err=%v", calls, err)
			}
		})
	}
	for _, bad := range []string{"default", "", "../invalid"} {
		badSpec := a
		badSpec.ComposeProject = bad
		calls := 0
		err := ensureCredentialVolume(context.Background(), badSpec, ownedAuditProject(),
			func(context.Context, statevolume.GatewayCredentials) error { calls++; return nil })
		if err == nil || calls != 0 {
			t.Fatalf("invalid project %q called ensure: calls=%d err=%v", bad, calls, err)
		}
	}
}
