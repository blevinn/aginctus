package incus

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lxc/incus/v7/shared/api"
)

type fakeServer struct {
	server    *api.Server
	serverErr error
}

func (s *fakeServer) GetServer() (*api.Server, string, error) {
	return s.server, "", s.serverErr
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

func TestServerVersionQueryFailure(t *testing.T) {
	server := &fakeServer{serverErr: errors.New("query failed")}
	client := NewClientWithConnector(func(context.Context) (Server, error) { return server, nil })

	_, err := client.ServerVersion(context.Background())
	if err == nil || !strings.Contains(err.Error(), "query failed") {
		t.Fatalf("ServerVersion() error = %v", err)
	}
}
