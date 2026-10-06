package incus

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lxc/incus/v7/shared/api"
)

type fakeOperation struct {
	err error
}

func (o fakeOperation) Wait() error {
	return o.err
}

type fakeServer struct {
	server            *api.Server
	serverErr         error
	instance          *api.Instance
	instanceErr       error
	instanceETag      string
	createdInstance   *api.InstancesPost
	createdImageAlias string
	updatedInstance   *api.InstancePut
	deletedInstance   string
	stateChange       *api.InstanceStatePut
}

func (s *fakeServer) GetServer() (*api.Server, string, error) {
	return s.server, "", s.serverErr
}

func (s *fakeServer) GetInstance(string) (*api.Instance, string, error) {
	return s.instance, s.instanceETag, s.instanceErr
}

func (s *fakeServer) CreateInstanceFromLocalImage(alias string, instance api.InstancesPost) (operation, error) {
	s.createdImageAlias = alias
	s.createdInstance = &instance
	return fakeOperation{}, nil
}

func (s *fakeServer) UpdateInstance(_ string, instance api.InstancePut, _ string) (operation, error) {
	s.updatedInstance = &instance
	return fakeOperation{}, nil
}

func (s *fakeServer) DeleteInstance(name string) (operation, error) {
	s.deletedInstance = name
	return fakeOperation{}, nil
}

func (s *fakeServer) UpdateInstanceState(_ string, state api.InstanceStatePut, _ string) (operation, error) {
	s.stateChange = &state
	return fakeOperation{}, nil
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

func TestEnsureHerdrClientCreatesContainer(t *testing.T) {
	server := &fakeServer{instanceErr: api.StatusErrorf(404, "not found")}
	client := NewClientWithConnector(func(context.Context) (Server, error) { return server, nil })

	spec := HerdrClientSpec{
		Name:              "aginctus-herdr",
		ImageAlias:        "aginctus-herdr-client",
		StoragePool:       "default",
		ManagementNetwork: "aginctus-mgmt",
	}

	result, err := client.EnsureHerdrClient(context.Background(), spec, MutationOptions{})
	if err != nil {
		t.Fatalf("EnsureHerdrClient() error = %v", err)
	}
	if !result.Created || server.createdInstance == nil {
		t.Fatalf("result = %#v request = %#v", result, server.createdInstance)
	}
	if server.createdInstance.Type != api.InstanceTypeContainer {
		t.Fatalf("type = %q", server.createdInstance.Type)
	}
	if server.createdImageAlias != "aginctus-herdr-client" {
		t.Fatalf("image alias = %q", server.createdImageAlias)
	}
	if got := server.createdInstance.Devices["management"]["network"]; got != "aginctus-mgmt" {
		t.Fatalf("management network = %q", got)
	}
	if got := server.createdInstance.Devices["root"]["pool"]; got != "default" {
		t.Fatalf("storage pool = %q", got)
	}
	if len(server.createdInstance.Profiles) != 0 {
		t.Fatalf("profiles = %#v, want none", server.createdInstance.Profiles)
	}
}

func TestEnsureHerdrClientDryRunDoesNotCreate(t *testing.T) {
	server := &fakeServer{instanceErr: api.StatusErrorf(404, "not found")}
	client := NewClientWithConnector(func(context.Context) (Server, error) { return server, nil })

	result, err := client.EnsureHerdrClient(context.Background(), HerdrClientSpec{Name: "aginctus-herdr"}, MutationOptions{DryRun: true})
	if err != nil {
		t.Fatalf("EnsureHerdrClient() error = %v", err)
	}
	if !result.Created || !result.DryRun {
		t.Fatalf("result = %#v", result)
	}
	if server.createdInstance != nil {
		t.Fatal("CreateInstance() called during dry run")
	}
}

func TestEnsureHerdrClientRejectsUnownedContainer(t *testing.T) {
	server := &fakeServer{instance: &api.Instance{
		Name:        "aginctus-herdr",
		Type:        string(api.InstanceTypeContainer),
		InstancePut: api.InstancePut{Config: api.ConfigMap{}},
	}}
	client := NewClientWithConnector(func(context.Context) (Server, error) { return server, nil })

	_, err := client.EnsureHerdrClient(context.Background(), HerdrClientSpec{Name: "aginctus-herdr"}, MutationOptions{})
	if err == nil || !strings.Contains(err.Error(), "use --force") {
		t.Fatalf("EnsureHerdrClient() error = %v", err)
	}
}

func TestTeardownHerdrClientDeletesOwnedContainer(t *testing.T) {
	server := &fakeServer{instance: &api.Instance{
		Name: "aginctus-herdr",
		Type: string(api.InstanceTypeContainer),
		InstancePut: api.InstancePut{Config: api.ConfigMap{
			ownerKey:    ownerValue,
			resourceKey: "infrastructure",
			roleKey:     herdrRoleValue,
		}},
	}}
	client := NewClientWithConnector(func(context.Context) (Server, error) { return server, nil })

	result, err := client.TeardownHerdrClient(context.Background(), "aginctus-herdr", MutationOptions{})
	if err != nil {
		t.Fatalf("TeardownHerdrClient() error = %v", err)
	}
	if !result.Deleted || server.deletedInstance != "aginctus-herdr" {
		t.Fatalf("result = %#v deleted = %q", result, server.deletedInstance)
	}
	if server.stateChange == nil || server.stateChange.Action != "stop" {
		t.Fatalf("state change = %#v", server.stateChange)
	}
}
