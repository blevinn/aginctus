package gateway

import (
	"context"
	"fmt"
	"regexp"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
)

var gatewaySecretVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// NativeGatewayRuntimeBinding identifies one environment's accepted revision
// within its Incus project's authority. Credential version and exact resource
// ownership are carried without secrets, paths, or any implied start grant.
type NativeGatewayRuntimeBinding struct {
	Resources                   NativeGatewayResourceIdentity `json:"resources"`
	AcceptedEnvironmentRevision environment.AcceptedRevision  `json:"acceptedEnvironmentRevision"`
	CredentialVersion           string                        `json:"credentialVersion"`
	Images                      NativeGatewayCreateImages     `json:"images"`
	RootPool                    string                        `json:"rootPool"`
	DatabasePool                string                        `json:"databasePool"`
}

// Validate requires a canonical verified server/project environment binding
// and the same owner and exact guest identities throughout the gateway plan.
// AcceptedEnvironmentRevision is a pointer into the project-scoped accepted
// state service; this function does not authenticate or retrieve that pointer.
func (b NativeGatewayRuntimeBinding) Validate(descriptor environment.Descriptor, selected environment.Binding, pair NativeGatewayInstancePair) error {
	if err := b.Resources.ValidateEnvironmentIdentity(descriptor, selected); err != nil {
		return err
	}
	if err := validateNativeGatewayPairOwner(pair); err != nil {
		return err
	}
	if pair.Postgres.EnvironmentID != b.Resources.EnvironmentID ||
		pair.LiteLLM.EnvironmentID != b.Resources.EnvironmentID ||
		pair.Postgres.Project != b.Resources.Project || pair.LiteLLM.Project != b.Resources.Project ||
		pair.Postgres.Name != b.Resources.PostgresInstance ||
		pair.LiteLLM.Name != b.Resources.LiteLLMInstance ||
		b.AcceptedEnvironmentRevision.ValidateIdentity() != nil ||
		b.AcceptedEnvironmentRevision.EnvironmentID != descriptor.ID ||
		b.AcceptedEnvironmentRevision.ServerID != selected.ServerID ||
		b.AcceptedEnvironmentRevision.Project != selected.Project ||
		!nativeDeviceName.MatchString(b.RootPool) || !nativeDeviceName.MatchString(b.DatabasePool) ||
		pair.Postgres.Devices["root"]["pool"] != b.RootPool || pair.LiteLLM.Devices["root"]["pool"] != b.RootPool ||
		pair.Postgres.Devices["database"]["pool"] != b.DatabasePool ||
		b.Images.Postgres.Evidence.Alias != pair.Postgres.ImageAlias || b.Images.LiteLLM.Evidence.Alias != pair.LiteLLM.ImageAlias ||
		!gatewaySecretVersion.MatchString(b.CredentialVersion) {
		return fmt.Errorf("native gateway accepted revision, credential version, or environment guest binding unverified")
	}
	return nil
}

// NativeGatewayDeliveryVerifier must be supplied by an independently trusted
// accepted-state and secret-delivery authority. It must re-read the project's
// environment revision pointer and validate actual guest-local delivery,
// current credential version, actor authority and live writer lease.
// An implementation supplied by an untrusted caller cannot establish trust.
type NativeGatewayDeliveryVerifier interface {
	VerifyGatewayDelivery(context.Context, NativeGatewayRuntimeBinding) error
}

// VerifyNativeGatewayRuntimeBinding is fail-closed and redacts verifier errors.
// Values in the binding do NOT attest to their own correctness. In particular,
// this function neither reads Incus environment.* config nor starts a guest.
func VerifyNativeGatewayRuntimeBinding(ctx context.Context, descriptor environment.Descriptor, selected environment.Binding, pair NativeGatewayInstancePair, b NativeGatewayRuntimeBinding, verifier NativeGatewayDeliveryVerifier) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := b.Validate(descriptor, selected, pair); err != nil {
		return err
	}
	if verifier == nil {
		return fmt.Errorf("trusted native gateway delivery verifier unavailable")
	}
	if err := verifier.VerifyGatewayDelivery(ctx, b); err != nil {
		return fmt.Errorf("native gateway accepted revision or credential delivery unverified")
	}
	return ctx.Err()
}
