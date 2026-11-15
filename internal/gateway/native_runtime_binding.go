package gateway

import (
	"context"
	"fmt"
	"regexp"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

var gatewayRevisionID = regexp.MustCompile(`^[a-f0-9]{64}$`)
var gatewayCredentialVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// NativeGatewayRuntimeBinding names the exact protected accepted revision and
// credential version for both services. The current fixed-name gateway is
// explicitly a project-owned resource; this is NOT an environment gateway
// identity or a grant to modify host or other project resources.
// It contains no secret bytes or paths.
// These identifiers alone are NOT evidence of a delivery or a startup grant.
type NativeGatewayRuntimeBinding struct {
	ResourceScope     string `json:"resourceScope"`
	Project           string `json:"project"`
	AcceptedRevision  string `json:"acceptedRevision"`
	CredentialVersion string `json:"credentialVersion"`
	PostgresInstance  string `json:"postgresInstance"`
	LiteLLMInstance   string `json:"litellmInstance"`
}

func (b NativeGatewayRuntimeBinding) Validate(pair NativeGatewayInstancePair) error {
	if _, err := incusscope.NewProjectScope(b.Project); err != nil {
		return fmt.Errorf("gateway runtime binding requires an explicit owned project")
	}
	if b.ResourceScope != "project" || b.Project != pair.Postgres.Project || b.Project != pair.LiteLLM.Project ||
		b.PostgresInstance != "postgres-1" || b.LiteLLMInstance != "litellm-1" ||
		b.PostgresInstance != pair.Postgres.Name || b.LiteLLMInstance != pair.LiteLLM.Name ||
		!gatewayRevisionID.MatchString(b.AcceptedRevision) ||
		!gatewayCredentialVersion.MatchString(b.CredentialVersion) {
		return fmt.Errorf("gateway runtime binding scope, project, service, revision or credential version unverified")
	}
	return nil
}

// NativeGatewayRuntimeVerifier is implemented by the independently trusted
// credential delivery authority, distinct from the project-scoped accepted
// state authority. It must check the exact project-wide record, revision, writer
// principal/lease, actual guest delivery and matching credential version.
// This interface does not provide a default verifier or accept a bool receipt.
// Verifier implementations must avoid returning secret-bearing errors.
type NativeGatewayRuntimeVerifier interface {
	VerifyGatewayRuntime(context.Context, NativeGatewayRuntimeBinding) error
}

// VerifyNativeGatewayRuntimeBinding validates exact service identities before
// invoking independent credential-delivery verification. It does not treat
// Incus environment keys, untrusted manifest metadata or caller-asserted
// booleans as evidence of secure delivery.
func VerifyNativeGatewayRuntimeBinding(ctx context.Context, pair NativeGatewayInstancePair, b NativeGatewayRuntimeBinding, verifier NativeGatewayRuntimeVerifier) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := b.Validate(pair); err != nil {
		return err
	}
	if verifier == nil {
		return fmt.Errorf("gateway runtime requires independently trusted delivery verifier")
	}
	if err := verifier.VerifyGatewayRuntime(ctx, b); err != nil {
		return fmt.Errorf("gateway runtime delivery evidence unverified")
	}
	return ctx.Err()
}
