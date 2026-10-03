package incus

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lxc/incus/v7/shared/api"
)

type fakeServer struct {
	server *api.Server
	err    error
}

func (s fakeServer) GetServer() (*api.Server, string, error) {
	return s.server, "", s.err
}

func TestServerVersion(t *testing.T) {
	client := NewClientWithConnector(func(context.Context) (Server, error) {
		return fakeServer{
			server: &api.Server{
				Environment: api.ServerEnvironment{
					ServerVersion: "7.0.1",
				},
			},
		}, nil
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
	if err == nil {
		t.Fatal("ServerVersion() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("ServerVersion() error = %q", err)
	}
}

func TestServerVersionGetServerFailure(t *testing.T) {
	client := NewClientWithConnector(func(context.Context) (Server, error) {
		return fakeServer{err: errors.New("connection closed")}, nil
	})

	_, err := client.ServerVersion(context.Background())
	if err == nil {
		t.Fatal("ServerVersion() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "connection closed") {
		t.Fatalf("ServerVersion() error = %q", err)
	}
}
