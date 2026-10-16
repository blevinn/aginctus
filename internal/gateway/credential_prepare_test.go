package gateway

import (
	"strings"
	"testing"

	"github.com/lxc/incus/v7/shared/api"
)

func preparationFixtures() (*api.Instance, *api.StorageVolume, CredentialAttachment) {
	a := attachmentFixture()
	inst := ownedInstance()
	inst.Status = "Running"
	inst.Config["oci.uid"] = "0"
	inst.Config["oci.gid"] = "0"
	inst.ExpandedDevices[credentialDevice] = a.device()
	vol := &api.StorageVolume{
		Name: a.volume().Name(), Type: "custom", ContentType: "filesystem",
		StorageVolumePut: api.StorageVolumePut{Config: map[string]string{
			"user.aginctus.managed": "true",
			"user.aginctus.resource": "gateway-credentials",
			"user.aginctus.gateway": "local",
		}},
		UsedBy: []string{"/1.0/instances/litellm-1?project=aginctus-gateway"},
	}
	return inst, vol, a
}

func TestCredentialPreparationTargetAcceptsPrivateMountedRootRuntime(t *testing.T) {
	inst, vol, a := preparationFixtures()
	if err := verifyCredentialPreparationTarget(inst, vol, a); err != nil {
		t.Fatal(err)
	}
	inst.Config["environment.CHATGPT_TOKEN_DIR"] = credentialMountPath
	inst.Config["environment.CHATGPT_AUTH_FILE"] = "auth.json"
	if err := verifyCredentialPreparationTarget(inst, vol, a); err != nil {
		t.Fatalf("expected future active settings to be compatible: %v", err)
	}
}

func TestCredentialPreparationTargetFailsClosed(t *testing.T) {
	cases := []struct {
		name string
		change func(*api.Instance, *api.StorageVolume, *CredentialAttachment)
	}{
		{"mount missing", func(i *api.Instance, _ *api.StorageVolume, _ *CredentialAttachment) { delete(i.ExpandedDevices, credentialDevice) }},
		{"wrong mount", func(i *api.Instance, _ *api.StorageVolume, _ *CredentialAttachment) { i.ExpandedDevices[credentialDevice]["source"] = "foreign" }},
		{"extra duplicate mount", func(i *api.Instance, _ *api.StorageVolume, a *CredentialAttachment) { i.ExpandedDevices["other"] = map[string]string{"type":"disk", "pool":a.Pool,"source":a.volume().Name(),"path":"/secret-copy"} }},
		{"wrong instance ownership", func(i *api.Instance, _ *api.StorageVolume, _ *CredentialAttachment) { i.Config["user.aginctus.managed"] = "false" }},
		{"foreign consumer", func(_ *api.Instance, v *api.StorageVolume, _ *CredentialAttachment) { v.UsedBy = append(v.UsedBy, "/1.0/instances/postgres-1?project=aginctus-gateway") }},
		{"volume unreferenced", func(_ *api.Instance, v *api.StorageVolume, _ *CredentialAttachment) { v.UsedBy = nil }},
		{"volume ownership invalid", func(_ *api.Instance, v *api.StorageVolume, _ *CredentialAttachment) { v.Config["user.aginctus.gateway"] = "other" }},
		{"stopped guest", func(i *api.Instance, _ *api.StorageVolume, _ *CredentialAttachment) { i.Status = "Stopped" }},
		{"unexpected uid", func(i *api.Instance, _ *api.StorageVolume, _ *CredentialAttachment) { i.Config["oci.uid"] = "1000" }},
		{"unexpected gid", func(i *api.Instance, _ *api.StorageVolume, _ *CredentialAttachment) { i.Config["oci.gid"] = "1000" }},
		{"alternate token dir", func(i *api.Instance, _ *api.StorageVolume, _ *CredentialAttachment) { i.Config["environment.CHATGPT_TOKEN_DIR"] = "/root/.config/litellm/chatgpt" }},
		{"alternate auth file", func(i *api.Instance, _ *api.StorageVolume, _ *CredentialAttachment) { i.Config["environment.CHATGPT_AUTH_FILE"] = "other.json" }},
		{"unsafe project", func(_ *api.Instance, _ *api.StorageVolume, a *CredentialAttachment) { a.ComposeProject = "default" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inst, vol, a := preparationFixtures()
			tc.change(inst, vol, &a)
			if err := verifyCredentialPreparationTarget(inst, vol, a); err == nil {
				t.Fatal("accepted unsafe preparation")
			}
		})
	}
}

func TestCredentialPreparationScriptHardensBeforeAnyAuthentication(t *testing.T) {
	for _, fragment := range []string{
		"umask 077",
		"chmod 0700",
		"set -C; printf '{}",
		"stat -c '%a:%u:%g'",
		"mktemp",
		"trap 'rm -f",
	} {
		if !strings.Contains(credentialPreparationScript, fragment) {
			t.Errorf("preparation script missing security guard %q", fragment)
		}
	}
	for _, forbidden := range []string{"curl ", "python ", "get_access_token", "cat \"$auth\"", "printenv", "chmod 777"} {
		if strings.Contains(credentialPreparationScript, forbidden) {
			t.Fatalf("preparation script contains forbidden operation")
		}
	}
}
