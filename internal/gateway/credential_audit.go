package gateway

import (
	"context"
	"fmt"

	"github.com/blevinn/aginctus/internal/statevolume"
	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

// auditCredentialAttachment checks only Incus metadata. It does not open the
// credential filesystem, inspect auth.json, or assert that OAuth is safe to use.
func auditCredentialAttachment(a CredentialAttachment, project *api.Project, instance *api.Instance, volume *api.StorageVolume) error {
	if err := a.Validate(); err != nil {
		return err
	}
	if err := verifyCredentialComposeProject(project, a.ComposeProject); err != nil {
		return err
	}
	needed, err := plannedCredentialDevice(instance, a)
	if err != nil {
		return err
	}
	if needed {
		return fmt.Errorf("owned LiteLLM instance has no matching credential device")
	}
	if err := statevolume.VerifyGatewayCredentials(a.volume(), volume); err != nil {
		return err
	}
	if err := volumeExclusivity(volume, a.ComposeProject); err != nil {
		return err
	}
	// Missing UsedBy metadata is not evidence of exclusive attachment; verify
	// Incus independently reports exactly one consumer, including stopped VMs.
	if len(volume.UsedBy) != 1 {
		return fmt.Errorf("credential volume must report exactly one LiteLLM attachment")
	}
	return nil
}

// AuditCredentialAttachment inspects only an existing owned Incus project,
// LiteLLM instance, and custom volume. No resources are created, updated,
// started, or stopped. In-guest file permissions, active worker count, native
// authenticator behavior, account authorization and OAuth are NOT verified.
func AuditCredentialAttachment(ctx context.Context, a CredentialAttachment) error {
	if err := a.Validate(); err != nil {
		return err
	}
	client, err := incusclient.ConnectIncusUnixWithContext(ctx, "", &incusclient.ConnectionArgs{SkipGetServer: true})
	if err != nil {
		return fmt.Errorf("connect to local Incus for read-only credential audit: %w", err)
	}
	project, _, err := client.GetProject(a.ComposeProject)
	if err != nil {
		return fmt.Errorf("inspect gateway Compose project: unavailable")
	}
	if err := verifyCredentialComposeProject(project, a.ComposeProject); err != nil {
		return err
	}
	server := client.UseProject(a.ComposeProject)
	instance, _, err := server.GetInstance(liteLLMServiceInstance)
	if err != nil {
		return fmt.Errorf("inspect owned LiteLLM instance: unavailable")
	}
	volume, _, err := server.GetStoragePoolVolume(a.Pool, "custom", a.volume().Name())
	if err != nil {
		return fmt.Errorf("inspect project-local credential volume: unavailable")
	}
	return auditCredentialAttachment(a, project, instance, volume)
}
