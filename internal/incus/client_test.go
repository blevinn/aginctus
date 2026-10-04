package incus

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lxc/incus/v7/shared/api"
)

type fakeServer struct {
	server       *api.Server
	serverErr    error
	network      *api.Network
	networkErr   error
	etag         string
	created      *api.NetworksPost
	updatedName  string
	updatedPut   *api.NetworkPut
	updateETag   string
	deletedName  string
}

func (s *fakeServer) GetServer() (*api.Server, string, error) {
	return s.server, "", s.serverErr
}

func (s *fakeServer) GetNetwork(string) (*api.Network, string, error) {
	return s.network, s.etag, s.networkErr
}

func (s *fakeServer) CreateNetwork(network api.NetworksPost) error {
	s.created = &network
	return nil
}

func (s *fakeServer) UpdateNetwork(name string, network api.NetworkPut, etag string) error {
	s.updatedName = name
	s.updatedPut = &network
	s.updateETag = etag
	return nil
}

func (s *fakeServer) DeleteNetwork(name string) error {
	s.deletedName = name
	return nil
}

func TestServerVersion(t *testing.T) {
	server := &fakeServer{server: &api.Server{Environment: api.ServerEnvironment{ServerVersion: "7.0.1"}}}
	client := NewClientWithConnector(func(context.Context) (Server, error) { return server, nil })

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

func TestEnsureManagementNetworkCreatesConfiguredNetwork(t *testing.T) {
	server := &fakeServer{networkErr: api.StatusErrorf(404, "not found")}
	client := NewClientWithConnector(func(context.Context) (Server, error) { return server, nil })

	spec := ManagementNetworkSpec{
		Name: "lab-mgmt",
		IPv4Address: "10.42.0.1/24",
		IPv4NAT: true,
		IPv4Routing: false,
		IPv6Address: "none",
	}

	result, err := client.EnsureManagementNetwork(context.Background(), spec, MutationOptions{})
	if err != nil {
		t.Fatalf("EnsureManagementNetwork() error = %v", err)
	}
	if !result.Created || result.Updated || server.created == nil {
		t.Fatalf("result = %#v, request = %#v", result, server.created)
	}
	if server.created.Name != spec.Name || server.created.Config["ipv4.address"] != spec.IPv4Address {
		t.Fatalf("created network = %#v", server.created)
	}
	if server.created.Config["ipv4.nat"] != "true" || server.created.Config["ipv4.routing"] != "false" {
		t.Fatalf("network policy = %#v", server.created.Config)
	}
	if server.created.Config[ownerKey] != ownerValue || server.created.Config[resourceKey] != resourceValue {
		t.Fatalf("ownership metadata = %#v", server.created.Config)
	}
}

func TestEnsureManagementNetworkLeavesMatchingOwnedNetworkUnchanged(t *testing.T) {
	spec := ManagementNetworkSpec{
		Name: "lab-mgmt",
		IPv4Address: "10.42.0.1/24",
		IPv4NAT: false,
		IPv4Routing: false,
		IPv6Address: "none",
	}
	server := &fakeServer{network: &api.Network{
		Name: "lab-mgmt",
		Type: "bridge",
		Managed: true,
		NetworkPut: api.NetworkPut{Config: api.ConfigMap{
			"ipv4.address": "10.42.0.1/24",
			"ipv4.nat": "false",
			"ipv4.routing": "false",
			"ipv6.address": "none",
			ownerKey: ownerValue,
			resourceKey: resourceValue,
		}},
	}}
	client := NewClientWithConnector(func(context.Context) (Server, error) { return server, nil })

	result, err := client.EnsureManagementNetwork(context.Background(), spec, MutationOptions{})
	if err != nil {
		t.Fatalf("EnsureManagementNetwork() error = %v", err)
	}
	if result.Created || result.Updated {
		t.Fatalf("result = %#v, want unchanged", result)
	}
	if server.updatedPut != nil {
		t.Fatal("UpdateNetwork() called for matching network")
	}
}

func TestEnsureManagementNetworkUpdatesOwnedNetwork(t *testing.T) {
	server := &fakeServer{
		etag: "etag-1",
		network: &api.Network{
			Name: "lab-mgmt",
			Type: "bridge",
			Managed: true,
			NetworkPut: api.NetworkPut{Config: api.ConfigMap{
				"ipv4.address": "10.42.0.1/24",
				"ipv4.nat": "false",
				"ipv4.routing": "false",
				"ipv6.address": "none",
				ownerKey: ownerValue,
				resourceKey: resourceValue,
			}},
		},
	}
	client := NewClientWithConnector(func(context.Context) (Server, error) { return server, nil })

	result, err := client.EnsureManagementNetwork(context.Background(), ManagementNetworkSpec{
		Name: "lab-mgmt",
		IPv4Address: "10.42.0.1/24",
		IPv4NAT: true,
		IPv4Routing: false,
		IPv6Address: "none",
	}, MutationOptions{})
	if err != nil {
		t.Fatalf("EnsureManagementNetwork() error = %v", err)
	}
	if result.Created || !result.Updated {
		t.Fatalf("result = %#v, want updated", result)
	}
	if server.updatedPut == nil || server.updatedPut.Config["ipv4.nat"] != "true" {
		t.Fatalf("updated network = %#v", server.updatedPut)
	}
	if server.updatedName != "lab-mgmt" || server.updateETag != "etag-1" {
		t.Fatalf("update target = %q, etag = %q", server.updatedName, server.updateETag)
	}
}

func TestEnsureManagementNetworkRejectsUnownedCollision(t *testing.T) {
	server := &fakeServer{network: &api.Network{
		Name: "lab-mgmt",
		Type: "bridge",
		Managed: true,
		NetworkPut: api.NetworkPut{Config: api.ConfigMap{}},
	}}
	client := NewClientWithConnector(func(context.Context) (Server, error) { return server, nil })

	_, err := client.EnsureManagementNetwork(context.Background(), ManagementNetworkSpec{Name: "lab-mgmt"}, MutationOptions{})
	if err == nil || !strings.Contains(err.Error(), "not owned by Aginctus") {
		t.Fatalf("EnsureManagementNetwork() error = %v", err)
	}
}


func TestEnsureManagementNetworkDryRunDoesNotCreate(t *testing.T) {
	server := &fakeServer{networkErr: api.StatusErrorf(404, "not found")}
	client := NewClientWithConnector(func(context.Context) (Server, error) { return server, nil })

	result, err := client.EnsureManagementNetwork(context.Background(), ManagementNetworkSpec{Name: "lab-mgmt"}, MutationOptions{DryRun: true})
	if err != nil {
		t.Fatalf("EnsureManagementNetwork() error = %v", err)
	}
	if !result.Created || !result.DryRun {
		t.Fatalf("result = %#v", result)
	}
	if server.created != nil {
		t.Fatal("CreateNetwork() called during dry run")
	}
}

func TestEnsureManagementNetworkForceAdoptsUnownedBridge(t *testing.T) {
	server := &fakeServer{network: &api.Network{
		Name: "lab-mgmt",
		Type: "bridge",
		Managed: true,
		NetworkPut: api.NetworkPut{Config: api.ConfigMap{}},
	}}
	client := NewClientWithConnector(func(context.Context) (Server, error) { return server, nil })

	result, err := client.EnsureManagementNetwork(
		context.Background(),
		ManagementNetworkSpec{Name: "lab-mgmt", IPv4Address: "auto", IPv6Address: "none"},
		MutationOptions{Force: true},
	)
	if err != nil {
		t.Fatalf("EnsureManagementNetwork() error = %v", err)
	}
	if !result.Updated {
		t.Fatalf("result = %#v, want updated", result)
	}
	if server.updatedPut == nil || server.updatedPut.Config[ownerKey] != ownerValue {
		t.Fatalf("updated network = %#v", server.updatedPut)
	}
}

func TestTeardownManagementNetworkDeletesOwnedNetwork(t *testing.T) {
	server := &fakeServer{network: &api.Network{
		Name: "lab-mgmt",
		Type: "bridge",
		Managed: true,
		NetworkPut: api.NetworkPut{Config: api.ConfigMap{
			ownerKey: ownerValue,
			resourceKey: resourceValue,
		}},
	}}
	client := NewClientWithConnector(func(context.Context) (Server, error) { return server, nil })

	result, err := client.TeardownManagementNetwork(context.Background(), "lab-mgmt", MutationOptions{})
	if err != nil {
		t.Fatalf("TeardownManagementNetwork() error = %v", err)
	}
	if !result.Deleted || result.DryRun {
		t.Fatalf("result = %#v", result)
	}
	if server.deletedName != "lab-mgmt" {
		t.Fatalf("deletedName = %q", server.deletedName)
	}
}

func TestTeardownManagementNetworkDryRunDoesNotDelete(t *testing.T) {
	server := &fakeServer{network: &api.Network{
		Name: "lab-mgmt",
		Type: "bridge",
		Managed: true,
		NetworkPut: api.NetworkPut{Config: api.ConfigMap{
			ownerKey: ownerValue,
			resourceKey: resourceValue,
		}},
	}}
	client := NewClientWithConnector(func(context.Context) (Server, error) { return server, nil })

	result, err := client.TeardownManagementNetwork(context.Background(), "lab-mgmt", MutationOptions{DryRun: true})
	if err != nil {
		t.Fatalf("TeardownManagementNetwork() error = %v", err)
	}
	if !result.Deleted || !result.DryRun {
		t.Fatalf("result = %#v", result)
	}
	if server.deletedName != "" {
		t.Fatalf("DeleteNetwork() called during dry run: %q", server.deletedName)
	}
}

func TestTeardownManagementNetworkForceDeletesUnownedBridge(t *testing.T) {
	server := &fakeServer{network: &api.Network{
		Name: "lab-mgmt",
		Type: "bridge",
		Managed: true,
		NetworkPut: api.NetworkPut{Config: api.ConfigMap{}},
	}}
	client := NewClientWithConnector(func(context.Context) (Server, error) { return server, nil })

	result, err := client.TeardownManagementNetwork(context.Background(), "lab-mgmt", MutationOptions{Force: true})
	if err != nil {
		t.Fatalf("TeardownManagementNetwork() error = %v", err)
	}
	if !result.Deleted || server.deletedName != "lab-mgmt" {
		t.Fatalf("result = %#v deletedName = %q", result, server.deletedName)
	}
}

func TestTeardownManagementNetworkRejectsUnownedWithoutForce(t *testing.T) {
	server := &fakeServer{network: &api.Network{
		Name: "lab-mgmt",
		Type: "bridge",
		Managed: true,
		NetworkPut: api.NetworkPut{Config: api.ConfigMap{}},
	}}
	client := NewClientWithConnector(func(context.Context) (Server, error) { return server, nil })

	_, err := client.TeardownManagementNetwork(context.Background(), "lab-mgmt", MutationOptions{})
	if err == nil || !strings.Contains(err.Error(), "use --force") {
		t.Fatalf("TeardownManagementNetwork() error = %v", err)
	}
}
