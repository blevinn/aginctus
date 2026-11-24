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

type listenerServer struct {
	fakeServer
	etag string
	writes int
}

func (s *listenerServer) GetServer() (*api.Server, string, error) {
	return s.server, s.etag, s.serverErr
}

func (s *listenerServer) UpdateServer(put api.ServerPut, etag string) error {
	if etag != s.etag {
		return errors.New("unexpected revision")
	}
	s.writes++
	s.server.Config = put.Config
	return nil
}

func TestHTTPSRemediationUsesObservedConnection(t *testing.T) {
	for _, existing := range []string{"", "0.0.0.0:8443", "127.0.0.2:8443"} {
		s := &listenerServer{fakeServer: fakeServer{server: &api.Server{}}, etag: "revision"}
		s.server.Config = api.ConfigMap{"core.https_address": existing, "core.foo": "preserve"}
		connections := 0
		c := NewClientWithConnector(func(context.Context) (Server, error) {
			connections++
			return s, nil
		})
		err := c.SetHTTPSAddress(context.Background(), "127.0.0.1:8443")
		if connections != 1 || s.server.Config["core.foo"] != "preserve" {
			t.Fatal("remediation changed connection or unrelated configuration")
		}
		if existing == "" {
			if err != nil || s.writes != 1 || s.server.Config["core.https_address"] != "127.0.0.1:8443" {
				t.Fatalf("remediation failed: %v", err)
			}
		} else if err == nil || s.writes != 0 {
			t.Fatal("existing listener overwritten")
		}
	}
}
