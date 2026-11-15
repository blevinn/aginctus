package gateway

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration/native"
	"github.com/lxc/incus/v7/shared/api"
)

type gatewayStartFake struct {
	*fakeGatewayStoppedClient
	transitions []string
}

func (f *gatewayStartFake) UpdateInstanceState(name string, req api.InstanceStatePut, etag string) (native.InstanceOperation, error) {
	if req.Action != "start" || req.Force {
		return nil, fmt.Errorf("unexpected operation")
	}
	f.transitions = append(f.transitions, name)
	f.instances[name].Status = "Running"
	return successfulGatewayCreateOperation{}, nil
}

type gatewayReadyFunc func(context.Context, string, string) error

func (f gatewayReadyFunc) VerifyGatewayServiceReady(ctx context.Context, project, role string) error {
	return f(ctx, project, role)
}

func TestNativeGatewayStartFailsClosedUntilEnvironmentTopologyExists(t *testing.T) {
	top, devices, pair, client := stoppedClientFixture(t)
	if _, err := CreateStoppedGatewayWithClient(context.Background(), top, devices, pair, client, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	startClient := &gatewayStartFake{fakeGatewayStoppedClient: client}
	binding := NativeGatewayRuntimeBinding{
		ResourceScope: "environment", EnvironmentID: "env-alpha", Project: top.Project,
		AcceptedRevision: strings.Repeat("a", 64), CredentialVersion: "v1",
		PostgresInstance: "env-alpha-postgres", LiteLLMInstance: "env-alpha-litellm",
	}
	verifier := runtimeVerifierFunc(func(context.Context, NativeGatewayRuntimeBinding) error { return nil })
	readiness := gatewayReadyFunc(func(context.Context, string, string) error {
		t.Fatal("readiness called for legacy project-owned instances")
		return nil
	})
	result, err := StartNativeGatewayWithClient(context.Background(), top, devices, pair, startClient,
		func(context.Context) error { return nil }, binding, verifier, readiness)
	if err == nil || len(result.Started) != 0 || len(result.Ready) != 0 || len(startClient.transitions) != 0 {
		t.Fatalf("legacy topology was not rejected before writes: result=%+v err=%v transitions=%v", result, err, startClient.transitions)
	}
}
