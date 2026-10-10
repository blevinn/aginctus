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

func TestInstanceExecExitCode(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]any
		want     int
		wantErr  bool
	}{
		{name: "success", metadata: map[string]any{"return": float64(0)}, want: 0},
		{name: "failure", metadata: map[string]any{"return": float64(17)}, want: 17},
		{name: "integer", metadata: map[string]any{"return": 3}, want: 3},
		{name: "missing metadata", metadata: nil, wantErr: true},
		{name: "missing return", metadata: map[string]any{}, wantErr: true},
		{name: "unexpected type", metadata: map[string]any{"return": "0"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := instanceExecExitCode(tt.metadata)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("instanceExecExitCode() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("instanceExecExitCode() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("instanceExecExitCode() = %d, want %d", got, tt.want)
			}
		})
	}
}


func TestSelectInstanceAddress(t *testing.T) {
	tests := []struct {
		name      string
		addresses []api.InstanceStateNetworkAddress
		want      string
		wantOK    bool
	}{
		{
			name: "prefers global IPv4",
			addresses: []api.InstanceStateNetworkAddress{
				{Family: "inet6", Scope: "global", Address: "fd42::25"},
				{Family: "inet", Scope: "global", Address: "10.42.0.25"},
			},
			want:   "10.42.0.25",
			wantOK: true,
		},
		{
			name: "accepts private IPv4 with non-global scope",
			addresses: []api.InstanceStateNetworkAddress{
				{Family: "inet", Scope: "local", Address: "10.42.0.25"},
			},
			want:   "10.42.0.25",
			wantOK: true,
		},
		{
			name: "skips loopback and link-local",
			addresses: []api.InstanceStateNetworkAddress{
				{Family: "inet", Scope: "local", Address: "127.0.0.1"},
				{Family: "inet6", Scope: "link", Address: "fe80::1"},
			},
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := selectInstanceAddress(tt.addresses)
			if ok != tt.wantOK {
				t.Fatalf("selectInstanceAddress() ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("selectInstanceAddress() = %q, want %q", got, tt.want)
			}
		})
	}
}


