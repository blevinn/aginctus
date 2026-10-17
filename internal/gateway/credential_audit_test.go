package gateway

import (
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/statevolume"
	"github.com/lxc/incus/v7/shared/api"
)

func ownedAuditProject() *api.Project {
	return &api.Project{
		Name: "aginctus-gateway",
		ProjectPut: api.ProjectPut{Config: map[string]string{
			"user.aginctus.managed":    "true",
			"user.aginctus.resource":   "compose-project",
			"user.aginctus.deployment": "aginctus-gateway",
			"features.storage.volumes": "true",
		}},
	}
}

func ownedAuditVolume(a CredentialAttachment) *api.StorageVolume {
	return &api.StorageVolume{
		Name:        a.volume().Name(),
		Type:        "custom",
		ContentType: "filesystem",
		StorageVolumePut: api.StorageVolumePut{Config: map[string]string{
			"user.aginctus.managed":  "true",
			"user.aginctus.resource": "gateway-credentials",
			"user.aginctus.gateway":  a.GatewayID,
		}},
		UsedBy: []string{"/1.0/instances/litellm-1?project=aginctus-gateway"},
	}
}

func TestCredentialAuditAcceptsExactlyOneOwnedMount(t *testing.T) {
	a := attachmentFixture()
	p := ownedAuditProject()
	i := ownedInstance()
	i.ExpandedDevices[credentialDevice] = a.device()
	v := ownedAuditVolume(a)
	if err := auditCredentialAttachment(a, p, i, v); err != nil {
		t.Fatalf("single attached owned credential volume rejected: %v", err)
	}
	// This is a pure metadata test: it neither reads tokens nor interacts
	// with a running guest, including when its runtime is stopped.
}

func TestCredentialAuditFailsClosed(t *testing.T) {
	a := attachmentFixture()
	type fixture struct {
		p *api.Project
		i *api.Instance
		v *api.StorageVolume
	}
	normal := func() fixture {
		f := fixture{p: ownedAuditProject(), i: ownedInstance(), v: ownedAuditVolume(a)}
		f.i.ExpandedDevices[credentialDevice] = a.device()
		return f
	}

	cases := []struct {
		name string
		edit func(*fixture)
		want string
	}{
		{"missing project", func(f *fixture) { f.p = nil }, "ownership"},
		{"wrong project", func(f *fixture) { f.p.Name = "default" }, "ownership"},
		{"unowned project", func(f *fixture) { f.p.Config["user.aginctus.resource"] = "foreign" }, "ownership"},
		{"shared volumes feature", func(f *fixture) { f.p.Config["features.storage.volumes"] = "false" }, "isolate"},
		{"missing instance", func(f *fixture) { f.i = nil }, "LiteLLM target"},
		{"foreign instance", func(f *fixture) { f.i.Config["user.aginctus.managed"] = "false" }, "LiteLLM target"},
		{"unmounted", func(f *fixture) { delete(f.i.ExpandedDevices, credentialDevice) }, "no matching"},
		{"changed device", func(f *fixture) { f.i.ExpandedDevices[credentialDevice]["source"] = "foreign" }, "unexpected"},
		{"overlapping mount", func(f *fixture) {
			f.i.ExpandedDevices["other"] = map[string]string{"type": "disk", "path": "/var/lib/aginctus"}
		}, "overlaps"},
		{"missing volume", func(f *fixture) { f.v = nil }, "credential volume"},
		{"wrong volume", func(f *fixture) { f.v.Name = "foreign" }, "credential volume"},
		{"foreign volume", func(f *fixture) { f.v.Config["user.aginctus.gateway"] = "foreign" }, "ownership"},
		{"no consumer reported", func(f *fixture) { f.v.UsedBy = nil }, "exactly one"},
		{"duplicate consumer", func(f *fixture) { f.v.UsedBy = append(f.v.UsedBy, f.v.UsedBy[0]) }, "exactly one"},
		{"postgres consumer", func(f *fixture) { f.v.UsedBy = []string{"/1.0/instances/postgres-1?project=aginctus-gateway"} }, "outside"},
		{"foreign profile", func(f *fixture) { f.v.UsedBy = []string{"/1.0/profiles/default?project=aginctus-gateway"} }, "outside"},
		{"other project", func(f *fixture) { f.v.UsedBy = []string{"/1.0/instances/litellm-1?project=default"} }, "outside"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := normal()
			tc.edit(&f)
			err := auditCredentialAttachment(a, f.p, f.i, f.v)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q rejection, got %v", tc.want, err)
			}
		})
	}
	if err := statevolume.VerifyGatewayCredentials(a.volume(), ownedAuditVolume(a)); err != nil {
		t.Fatalf("volume fixture is not valid: %v", err)
	}
}
