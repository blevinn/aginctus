package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type runtimeVerifierFunc func(context.Context, NativeGatewayRuntimeBinding) error

func (f runtimeVerifierFunc) VerifyGatewayRuntime(ctx context.Context, b NativeGatewayRuntimeBinding) error {
	return f(ctx, b)
}

func TestRuntimeBindingRequiresIndependentVerifier(t *testing.T) {
	pair, _ := instanceReaderFixture(t)
	b := NativeGatewayRuntimeBinding{ResourceScope: "project", Project: pair.Postgres.Project,
		AcceptedRevision: strings.Repeat("a", 64), CredentialVersion: "v1",
		PostgresInstance: "postgres-1", LiteLLMInstance: "litellm-1"}
	calls := 0
	verifier := runtimeVerifierFunc(func(_ context.Context, got NativeGatewayRuntimeBinding) error {
		calls++
		if got != b {
			t.Fatal("binding changed")
		}
		return nil
	})
	if err := VerifyNativeGatewayRuntimeBinding(context.Background(), pair, b, verifier); err != nil || calls != 1 {
		t.Fatalf("verified binding: %v", err)
	}
	if err := VerifyNativeGatewayRuntimeBinding(context.Background(), pair, b, nil); err == nil {
		t.Fatal("nil verifier accepted")
	}
	b.CredentialVersion = ""
	if err := VerifyNativeGatewayRuntimeBinding(context.Background(), pair, b, verifier); err == nil || calls != 1 {
		t.Fatal("unversioned receipt accepted")
	}
}
func TestRuntimeBindingRejectsForeignOrRevokedAndRedactsErrors(t *testing.T) {
	pair, _ := instanceReaderFixture(t)
	b := NativeGatewayRuntimeBinding{ResourceScope: "project", Project: pair.Postgres.Project,
		AcceptedRevision: strings.Repeat("a", 64), CredentialVersion: "v1",
		PostgresInstance: "postgres-1", LiteLLMInstance: "litellm-1"}
	secret := "synthetic-private-token"
	deny := runtimeVerifierFunc(func(context.Context, NativeGatewayRuntimeBinding) error { return errors.New(secret) })
	err := VerifyNativeGatewayRuntimeBinding(context.Background(), pair, b, deny)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("missing redaction: %v", err)
	}
	b.Project = "default"
	if err := VerifyNativeGatewayRuntimeBinding(context.Background(), pair, b, deny); err == nil {
		t.Fatal("default project accepted")
	}
}

// Current fixed-name gateway lifecycle is intentionally project-owned only.
// Environment-specific gateway resources need explicit environment identity,
// namespacing and ownership in a future topology, not a second ACL.
func TestRuntimeBindingRejectsUnscopedOrEnvironmentGateway(t *testing.T) {
	pair, _ := instanceReaderFixture(t)
	b := NativeGatewayRuntimeBinding{ResourceScope: "project", Project: pair.Postgres.Project,
		AcceptedRevision: strings.Repeat("a", 64), CredentialVersion: "v1",
		PostgresInstance: "postgres-1", LiteLLMInstance: "litellm-1"}
	if err := b.Validate(pair); err != nil { t.Fatal(err) }
	for _, scope := range []string{"", "environment", "user", "system"} {
		b.ResourceScope = scope
		if err := b.Validate(pair); err == nil { t.Fatalf("accepted unsupported gateway scope %q", scope) }
	}
}
