package incus

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lxc/incus/v7/shared/api"
)

type fakeServer struct {
	server        *api.Server
	serverErr     error
	network       *api.Network
	networkErr    error
	created       *api.NetworksPost
	createErr     error
}

func (s *fakeServer) GetServer() (*api.Server, string, error) {
	return s.server, "", s.serverErr
}

func (s *fakeServer) GetNetwork(string) (*api.Network, string, error) {
	return s.network, "", s.networkErr
}

func (s *fakeServer) CreateNetwork(network api.NetworksPost) error {
	s.created = &network
	return s.createErr
}

func TestServerVersion(t *testing.T) {
	server := &fakeServer{
		server: &api.Server{
			Environment: api.ServerEnvironment{ServerVersion: "7.0.1"},
		},
	}
	client := NewClientWithConnector(func(context.Context) (Server, error) {
		return server, nil
	})

	version, err := client.ServerVersion(context.Background())
	if err != nil {
		t.Fatalf("ServerVersion() error = %v", err)
	}
	if version != "7.0.1" {
		t.Fatalf("ServerVersion() = %q, want %q", version, "7.0.1")
	}
}

func TestServerVersionConnectionFailure(t *testing.T) {
	client := NewClientWithConnector(func(context.Context) (Server, error) {
		return nil, errors.New("permission denied")
	})

	_, err := client.ServerVersion(context.Background())
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("ServerVersion() error = %v", err)
	}
}

func TestEnsureManagementNetworkCreatesMissingNetwork(t *testing.T) {
	server := &fakeServer{networkErr: api.StatusErrorf(404, "not found")}
	client := NewClientWithConnector(func(context.Context) (Server, error) {
		return server, nil
	})

	created, err := client.EnsureManagementNetwork(context.Background())
	if err != nil {
		t.Fatalf("EnsureManagementNetwork() error = %v", err)
	}
	if !created {
		t.Fatal("EnsureManagementNetwork() created = false, want true")
	}
	if server.created == nil {
		t.Fatal("CreateNetwork() was not called")
	}
	if server.created.Name != ManagementNetworkName || server.created.Type != "bridge" {
		t.Fatalf("created network = %#v", server.created)
	}
	if server.created.Config[ownerKey] != ownerValue || server.created.Config[resourceKey] != resourceValue {
		t.Fatalf("created network missing ownership metadata: %#v", server.created.Config)
	}
	if server.created.Config["ipv4.nat"] != "false" {
		t.Fatalf("ipv4.nat = %q, want false", server.created.Config["ipv4.nat"])
	}
}

func TestEnsureManagementNetworkAcceptsOwnedNetwork(t *testing.T) {
	server := &fakeServer{
		network: &api.Network{
			Name:    ManagementNetworkName,
			Type:    "bridge",
			Managed: true,
			NetworkPut: api.NetworkPut{
				Config: api.ConfigMap{
					ownerKey:    ownerValue,
					resourceKey: resourceValue,
				},
			},
		},
	}
	client := NewClientWithConnector(func(context.Context) (Server, error) {
		return server, nil
	})

	created, err := client.EnsureManagementNetwork(context.Background())
	if err != nil {
		t.Fatalf("EnsureManagementNetwork() error = %v", err)
	}
	if created {
		t.Fatal("EnsureManagementNetwork() created = true, want false")
	}
	if server.created != nil {
		t.Fatal("CreateNetwork() called for existing network")
	}
}

func TestEnsureManagementNetworkRejectsUnownedCollision(t *testing.T) {
	server := &fakeServer{
		network: &api.Network{
			Name:    ManagementNetworkName,
			Type:    "bridge",
			Managed: true,
			NetworkPut: api.NetworkPut{Config: api.ConfigMap{}},
		},
	}
	client := NewClientWithConnector(func(context.Context) (Server, error) {
		return server, nil
	})

	_, err := client.EnsureManagementNetwork(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not owned by Aginctus") {
		t.Fatalf("EnsureManagementNetwork() error = %v", err)
	}
}
