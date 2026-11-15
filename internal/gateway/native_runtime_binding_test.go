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
	pair.Postgres.Name = "env-alpha-postgres"
	pair.LiteLLM.Name = "env-alpha-litellm"
	b := NativeGatewayRuntimeBinding{ResourceScope: "environment", EnvironmentID: "env-alpha", Project: pair.Postgres.Project,
		AcceptedRevision: strings.Repeat("a", 64), CredentialVersion: "v1",
		PostgresInstance: "env-alpha-postgres", LiteLLMInstance: "env-alpha-litellm"}
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
	pair.Postgres.Name = "env-alpha-postgres"
	pair.LiteLLM.Name = "env-alpha-litellm"
	b := NativeGatewayRuntimeBinding{ResourceScope: "environment", EnvironmentID: "env-alpha", Project: pair.Postgres.Project,
		AcceptedRevision: strings.Repeat("a", 64), CredentialVersion: "v1",
		PostgresInstance: "env-alpha-postgres", LiteLLMInstance: "env-alpha-litellm"}
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

// Only environment-owned gateway bindings are valid; project-scope accepted
// state does not establish gateway resource ownership.
func TestRuntimeBindingRejectsUnscopedOrProjectGateway(t *testing.T) {
	pair, _ := instanceReaderFixture(t)
	pair.Postgres.Name = "env-alpha-postgres"
	pair.LiteLLM.Name = "env-alpha-litellm"
	b := NativeGatewayRuntimeBinding{ResourceScope: "environment", EnvironmentID: "env-alpha", Project: pair.Postgres.Project,
		AcceptedRevision: strings.Repeat("a", 64), CredentialVersion: "v1",
		PostgresInstance: "env-alpha-postgres", LiteLLMInstance: "env-alpha-litellm"}
	if err := b.Validate(pair); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"", "project", "user", "system"} {
		b.ResourceScope = scope
		if err := b.Validate(pair); err == nil {
			t.Fatalf("accepted unsupported gateway scope %q", scope)
		}
	}
}

func TestRuntimeBindingRejectsCrossEnvironmentAndLegacyNames(t *testing.T) {
	pair, _ := instanceReaderFixture(t)
	pair.Postgres.Name = "env-alpha-postgres"
	pair.LiteLLM.Name = "env-alpha-litellm"
	b := NativeGatewayRuntimeBinding{
		ResourceScope: "environment", EnvironmentID: "env-alpha", Project: pair.Postgres.Project,
		AcceptedRevision: strings.Repeat("a", 64), CredentialVersion: "v1",
		PostgresInstance: pair.Postgres.Name, LiteLLMInstance: pair.LiteLLM.Name,
	}
	b.EnvironmentID = ""
	if err := b.Validate(pair); err == nil { t.Fatal("missing environment accepted") }
	b.EnvironmentID = "env-alpha"
	b.PostgresInstance = "postgres-1"
	if err := b.Validate(pair); err == nil { t.Fatal("legacy project gateway accepted") }
	b.PostgresInstance = pair.Postgres.Name
	b.LiteLLMInstance = b.PostgresInstance
	if err := b.Validate(pair); err == nil { t.Fatal("duplicate guest identities accepted") }
	b.LiteLLMInstance = pair.LiteLLM.Name
	pair.Postgres.Name = "env-beta-postgres"
	if err := b.Validate(pair); err == nil { t.Fatal("foreign guest accepted") }
}
