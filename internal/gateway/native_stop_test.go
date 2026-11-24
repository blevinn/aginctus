package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration/native"
	"github.com/lxc/incus/v7/shared/api"
)

type gatewayStopFake struct {
	*fakeGatewayStoppedClient
	transitions []string
	failOn      string
}

func (f *gatewayStopFake) UpdateInstanceState(name string, req api.InstanceStatePut, etag string) (native.InstanceOperation, error) {
	f.transitions = append(f.transitions, name)
	if name == f.failOn {
		return nil, errors.New("synthetic stop failure")
	}
	if req.Action != "stop" || req.Force {
		return nil, errors.New("unexpected forced or start operation")
	}
	f.instances[name].Status = "Stopped"
	return successfulGatewayCreateOperation{}, nil
}
func stopFixture(t *testing.T) (NativeGatewayTopology, NativeGatewayDevices, NativeGatewayInstancePair, *gatewayStopFake) {
	t.Helper()
	top, _, _, base := stoppedClientFixture(t)
	// Native lifecycle now requires canonical environment ownership on every
	// resource. Build the environment-owned fixture used by the real stop path.
	descriptor := gatewayTestEnvironment("123e4567-e89b-42d3-a456-426614174000", top.Project)
	top, err := BindNativeGatewayTopology(top, descriptor, descriptor.Binding)
	if err != nil {
		t.Fatal(err)
	}
	devices, err := PlanNativeGatewayDevices(top, "rootpool", "datapool")
	if err != nil {
		t.Fatal(err)
	}
	pair, err := PlanNativeGatewayInstances(top, devices, "pg-image", "llm-image")
	if err != nil {
		t.Fatal(err)
	}
	base.volume.Name = top.DatabaseVolume
	base.volume.Config["user.aginctus.environment-id"] = top.EnvironmentID
	client := base
	_, err = CreateStoppedGatewayWithClient(context.Background(), top, devices, pair, client, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []NativeGatewayInstanceSpec{pair.Postgres, pair.LiteLLM} {
		s := spec.Name
		client.instances[s].Status = "Running"
	}
	return top, devices, pair, &gatewayStopFake{fakeGatewayStoppedClient: client}
}
func TestGatewayStopReverseOrder(t *testing.T) {
	top, devices, pair, client := stopFixture(t)
	guards := 0
	result, err := StopNativeGatewayWithClient(context.Background(), top, devices, pair, client, func(context.Context) error { guards++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changed) != 2 || result.Changed[0] != pair.LiteLLM.Name || result.Changed[1] != pair.Postgres.Name ||
		len(result.Verified) != 2 || guards != 2 {
		t.Fatalf("stop ordering: %+v guards=%d", result, guards)
	}
	result, err = StopNativeGatewayWithClient(context.Background(), top, devices, pair, client, func(context.Context) error { t.Fatal("unexpected no-op guard"); return nil })
	if err != nil || len(result.Changed) != 0 || len(result.Verified) != 2 {
		t.Fatalf("repeat stop: %+v %v", result, err)
	}
}
func TestGatewayStopNilGuardAndPartialFailure(t *testing.T) {
	top, devices, pair, client := stopFixture(t)
	if _, err := StopNativeGatewayWithClient(context.Background(), top, devices, pair, client, nil); err == nil || len(client.transitions) != 0 {
		t.Fatal("nil guard permitted stop")
	}
	client.failOn = pair.Postgres.Name
	result, err := StopNativeGatewayWithClient(context.Background(), top, devices, pair, client, func(context.Context) error { return nil })
	if err == nil || len(result.Changed) != 1 || result.Changed[0] != pair.LiteLLM.Name ||
		client.instances[pair.Postgres.Name].Status != "Running" {
		t.Fatalf("partial stop not reported: %+v %v", result, err)
	}
}
