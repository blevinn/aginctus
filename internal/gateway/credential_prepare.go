package gateway

import (
	"context"
	"fmt"
	"io"
	"os/exec"

	"github.com/blevinn/aginctus/internal/statevolume"
	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

// Only the private, exclusively mounted credential volume may hold LiteLLM's
// native ChatGPT auth.json. The pinned authenticator opens auth.json with
// ordinary "w", so it must exist with mode 0600 *before* first login.
const credentialPreparationScript = `set -eu
umask 077
dir=/var/lib/aginctus/chatgpt
auth="$dir/auth.json"
[ "$(id -u)" = 0 ]
[ "$(id -g)" = 0 ]
[ -d "$dir" ]
[ ! -L "$dir" ]
[ "$(stat -c '%u:%g' "$dir")" = '0:0' ]
chmod 0700 "$dir"
[ "$(stat -c '%a' "$dir")" = '700' ]
if [ -e "$auth" ] || [ -L "$auth" ]; then
    [ ! -L "$auth" ]
    [ -f "$auth" ]
    [ "$(stat -c '%a:%u:%g' "$auth")" = '600:0:0' ]
else
    ( set -C; printf '{}\n' > "$auth" )
fi
[ ! -L "$auth" ]
[ -f "$auth" ]
[ "$(stat -c '%a:%u:%g' "$auth")" = '600:0:0' ]
[ -w "$auth" ]
probe="$(mktemp "$dir/.aginctus-write-check.XXXXXXXX")"
trap 'rm -f "$probe"' EXIT
[ "$(stat -c '%a:%u:%g' "$probe")" = '600:0:0' ]
`

// verifyCredentialPreparationTarget rejects unowned, missing, non-exclusive or
// misconfigured mounts without reading or modifying any credential contents.
func verifyCredentialPreparationTarget(instance *api.Instance, volume *api.StorageVolume, a CredentialAttachment) error {
	if err := a.Validate(); err != nil {
		return err
	}
	needed, err := plannedCredentialDevice(instance, a)
	if err != nil {
		return err
	}
	if needed {
		return fmt.Errorf("credential disk is not attached to LiteLLM; run credential-storage attach first")
	}
	if err := statevolume.VerifyGatewayCredentials(a.volume(), volume); err != nil {
		return err
	}
	if err := volumeExclusivity(volume, a.ComposeProject); err != nil {
		return err
	}
	// Incus's UsedBy is an independent check that this volume really belongs
	// to the one allowed guest, not just a matching device configuration.
	want := "/1.0/instances/" + liteLLMServiceInstance + "?project=" + a.ComposeProject
	if len(volume.UsedBy) != 1 || volume.UsedBy[0] != want {
		return fmt.Errorf("credential volume must be exclusively attached to LiteLLM")
	}
	if instance.Status != "Running" {
		return fmt.Errorf("LiteLLM must be running for credential write preflight")
	}
	// The pinned v1.103.0 OCI image runs as root. Do not mutate volume
	// ownership based on assumptions about an arbitrary runtime UID.
	if instance.Config["oci.uid"] != "0" || instance.Config["oci.gid"] != "0" {
		return fmt.Errorf("LiteLLM image runtime uid/gid must be 0 for this preparation flow")
	}
	for key, expected := range map[string]string{
		"environment.CHATGPT_TOKEN_DIR": credentialMountPath,
		"environment.CHATGPT_AUTH_FILE": "auth.json",
	} {
		if actual := instance.Config[key]; actual != "" && actual != expected {
			return fmt.Errorf("%s conflicts with managed credential state", key)
		}
	}
	return nil
}

// runPrivateGuestPreparation deliberately drops all child stdout/stderr: no
// credential values or application environment may enter host CLI output.
var runPrivateGuestPreparation = func(ctx context.Context, project string) error {
	cmd := exec.CommandContext(ctx, "incus", "--project", project,
		"exec", liteLLMServiceInstance, "--", "/bin/sh", "-c", credentialPreparationScript)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("private credential directory preparation failed (guest command error): %w", err)
	}
	return nil
}

// PrepareCredentialStorage explicitly makes the already-attached native
// credential directory private and checks that LiteLLM's pinned root user can
// write. It does not set CHATGPT_TOKEN_DIR, restart services or log in.
func PrepareCredentialStorage(ctx context.Context, a CredentialAttachment) error {
	if err := a.Validate(); err != nil {
		return err
	}
	client, err := incusclient.ConnectIncusUnixWithContext(ctx, "", &incusclient.ConnectionArgs{SkipGetServer: true})
	if err != nil {
		return fmt.Errorf("connect local Incus for credential preparation: %w", err)
	}
	project, _, err := client.GetProject(a.ComposeProject)
	if err != nil {
		return fmt.Errorf("inspect gateway Compose project: %w", err)
	}
	if project.Config["features.storage.volumes"] != "true" ||
		project.Config["user.aginctus.managed"] != "true" ||
		project.Config["user.aginctus.resource"] != "compose-project" ||
		project.Config["user.aginctus.deployment"] != a.ComposeProject {
		return fmt.Errorf("private credential preparation requires an owned, storage-isolated Compose project")
	}
	server := client.UseProject(a.ComposeProject)
	inst, _, err := server.GetInstance(liteLLMServiceInstance)
	if err != nil {
		return fmt.Errorf("inspect LiteLLM credential attachment: %w", err)
	}
	vol, _, err := server.GetStoragePoolVolume(a.Pool, "custom", a.volume().Name())
	if err != nil {
		return fmt.Errorf("inspect project-local credential volume: %w", err)
	}
	if err := verifyCredentialPreparationTarget(inst, vol, a); err != nil {
		return fmt.Errorf("credential preparation preflight: %w", err)
	}
	if err := runPrivateGuestPreparation(ctx, a.ComposeProject); err != nil {
		// Error reporting is intentionally free of guest output and tokens.
		return err
	}
	return nil
}
