package native

import (
	"context"
	"errors"
	"testing"

	"github.com/lxc/incus/v7/shared/api"
)

type fakeNetworks struct {
	networks []api.Network
	err      error
}

func (f fakeNetworks) GetNetworks() ([]api.Network, error) { return f.networks, f.err }
func TestNetworkObserverReadOnly(t *testing.T) {
	observer := NewNetworkObserverWithConnector(func(context.Context) (NetworkReader, error) {
		return fakeNetworks{networks: []api.Network{{Name: "managed", Config: map[string]string{ManagedKey: "true", ResourceKey: "network", "ipv4.nat": "false"}}}}, nil
	})
	resources := []Resource{{Kind: Network, Name: "managed", Config: map[string]string{"ipv4.nat": "false"}}, {Kind: Network, Name: "missing"}}
	observed, err := observer.ObserveNetworks(context.Background(), resources)
	if err != nil {
		t.Fatal(err)
	}
	if !observed[resources[0].ID()].Exists || observed[resources[1].ID()].Exists {
		t.Fatalf("unexpected observations: %+v", observed)
	}
	changes, err := Plan(resources, observed)
	if err != nil || changes[0].Action != Noop || changes[1].Action != Create {
		t.Fatalf("plan: %+v %v", changes, err)
	}
}
func TestNetworkObserverFailsClosed(t *testing.T) {
	sentinel := errors.New("offline")
	observer := NewNetworkObserverWithConnector(func(context.Context) (NetworkReader, error) { return fakeNetworks{err: sentinel}, nil })
	_, err := observer.ObserveNetworks(context.Background(), []Resource{{Kind: Network, Name: "one"}})
	if !errors.Is(err, sentinel) {
		t.Fatalf("lost connection failure: %v", err)
	}
	_, err = observer.ObserveNetworks(context.Background(), []Resource{{Kind: Instance, Name: "one"}})
	if err == nil {
		t.Fatal("unsupported instance inspection accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = observer.ObserveNetworks(ctx, []Resource{{Kind: Network, Name: "one"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled observer: %v", err)
	}
}
