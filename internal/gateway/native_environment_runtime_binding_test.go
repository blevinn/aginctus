package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
)

func fixtureGatewayRevision(t *testing.T, d environment.Descriptor) environment.AcceptedRevision {
	t.Helper()
	r, err := environment.BuildAcceptedRevision(nil, nil, environment.Candidate{
		APIVersion: environment.CandidateAPIVersion, Kind: environment.CandidateKind, Environment: d,
		SelectedWorkloads: []string{}, Workloads: []acceptedstate.WorkloadSpec{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

type gatewayDeliveryFunc func(context.Context, NativeGatewayRuntimeBinding) error

func (f gatewayDeliveryFunc) VerifyGatewayDelivery(ctx context.Context, b NativeGatewayRuntimeBinding) error {
	return f(ctx, b)
}
func gatewayRuntimeBindingFixture(t *testing.T) (environment.Descriptor, NativeGatewayInstancePair, NativeGatewayRuntimeBinding) {
	t.Helper()
	top := nativePinnedFixture()
	d := gatewayTestEnvironment("123e4567-e89b-42d3-a456-426614174000", top.Project)
	owned, err := BindNativeGatewayTopology(top, d, d.Binding)
	if err != nil {
		t.Fatal(err)
	}
	devices, err := PlanNativeGatewayDevices(owned, "rootpool", "datapool")
	if err != nil {
		t.Fatal(err)
	}
	pair, err := PlanNativeGatewayInstances(owned, devices, "pg-image", "llm-image")
	if err != nil {
		t.Fatal(err)
	}
	id, err := NativeGatewayIdentity(d, d.Binding)
	if err != nil {
		t.Fatal(err)
	}
	b := NativeGatewayRuntimeBinding{
		Resources: id, AcceptedEnvironmentRevision: fixtureGatewayRevision(t, d), CredentialVersion: "v1",
		RootPool: "rootpool", DatabasePool: "datapool",
	}
	b.Images.Postgres.Evidence.Alias = pair.Postgres.ImageAlias
	b.Images.LiteLLM.Evidence.Alias = pair.LiteLLM.ImageAlias
	return d, pair, b
}
func TestGatewayRuntimeBindingVerifiesCanonicalEnvironmentAndReceipt(t *testing.T) {
	desc, pair, b := gatewayRuntimeBindingFixture(t)
	calls := 0
	verify := gatewayDeliveryFunc(func(_ context.Context, got NativeGatewayRuntimeBinding) error {
		calls++
		if got != b {
			t.Fatal("binding changed")
		}
		return nil
	})
	if err := VerifyNativeGatewayRuntimeBinding(context.Background(), desc, desc.Binding, pair, b, verify); err != nil || calls != 1 {
		t.Fatalf("trusted verification not called: %v calls=%d", err, calls)
	}
	if err := VerifyNativeGatewayRuntimeBinding(context.Background(), desc, desc.Binding, pair, b, nil); err == nil {
		t.Fatal("missing delivery authority accepted")
	}
	b.CredentialVersion = ""
	if err := VerifyNativeGatewayRuntimeBinding(context.Background(), desc, desc.Binding, pair, b, verify); err == nil || calls != 1 {
		t.Fatal("unversioned credential accepted")
	}
}
func TestGatewayRuntimeBindingRejectsForeignSameProjectAndRevocation(t *testing.T) {
	desc, pair, b := gatewayRuntimeBindingFixture(t)
	other := gatewayTestEnvironment("123e4567-e89b-42d3-a456-426614174001", desc.Binding.Project)
	deny := gatewayDeliveryFunc(func(context.Context, NativeGatewayRuntimeBinding) error { return errors.New("synthetic-secret") })
	if err := VerifyNativeGatewayRuntimeBinding(context.Background(), other, other.Binding, pair, b, deny); err == nil {
		t.Fatal("same-project foreign environment accepted")
	}
	err := VerifyNativeGatewayRuntimeBinding(context.Background(), desc, desc.Binding, pair, b, deny)
	if err == nil || strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatalf("revocation leak or accepted: %v", err)
	}
	b.Resources.PostgresInstance = "postgres-1"
	if err := b.Validate(desc, desc.Binding, pair); err == nil {
		t.Fatal("legacy guest name accepted")
	}
}
