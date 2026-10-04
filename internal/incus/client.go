package incus

import (
	"context"
	"fmt"
	"net/http"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

const (
	ownerKey      = "user.aginctus.managed"
	resourceKey   = "user.aginctus.resource"
	ownerValue    = "true"
	resourceValue = "management-network"
)

type ManagementNetworkSpec struct {
	Name        string
	IPv4Address string
	IPv4NAT     bool
	IPv4Routing bool
	IPv6Address string
}

type Server interface {
	GetServer() (*api.Server, string, error)
	GetNetwork(string) (*api.Network, string, error)
	CreateNetwork(api.NetworksPost) error
}

type Connector func(context.Context) (Server, error)

type Client struct {
	connect Connector
}

func NewClient() *Client {
	return &Client{connect: func(ctx context.Context) (Server, error) {
		return incusclient.ConnectIncusUnixWithContext(ctx, "", &incusclient.ConnectionArgs{SkipGetServer: true})
	}}
}

func NewClientWithConnector(connect Connector) *Client {
	return &Client{connect: connect}
}

func (c *Client) ServerVersion(ctx context.Context) (string, error) {
	server, err := c.connect(ctx)
	if err != nil {
		return "", fmt.Errorf("connect to local Incus daemon: %w", err)
	}

	status, _, err := server.GetServer()
	if err != nil {
		return "", fmt.Errorf("get Incus server information: %w", err)
	}

	return status.Environment.ServerVersion, nil
}

func (c *Client) EnsureManagementNetwork(ctx context.Context, spec ManagementNetworkSpec) (bool, error) {
	server, err := c.connect(ctx)
	if err != nil {
		return false, fmt.Errorf("connect to local Incus daemon: %w", err)
	}

	network, _, err := server.GetNetwork(spec.Name)
	if err == nil {
		if err := validateManagementNetwork(network, spec.Name); err != nil {
			return false, err
		}
		return false, nil
	}
	if !api.StatusErrorCheck(err, http.StatusNotFound) {
		return false, fmt.Errorf("get management network %q: %w", spec.Name, err)
	}

	err = server.CreateNetwork(api.NetworksPost{
		Name: spec.Name,
		Type: "bridge",
		NetworkPut: api.NetworkPut{
			Description: "Aginctus management network",
			Config: api.ConfigMap{
				"ipv4.address": spec.IPv4Address,
				"ipv4.nat": boolString(spec.IPv4NAT),
				"ipv4.routing": boolString(spec.IPv4Routing),
				"ipv6.address": spec.IPv6Address,
				ownerKey: ownerValue,
				resourceKey: resourceValue,
			},
		},
	})
	if err != nil {
		return false, fmt.Errorf("create management network %q: %w", spec.Name, err)
	}

	return true, nil
}

func validateManagementNetwork(network *api.Network, name string) error {
	if !network.Managed {
		return fmt.Errorf("network %q already exists but is not Incus-managed", name)
	}
	if network.Type != "bridge" {
		return fmt.Errorf("network %q already exists with type %q, want bridge", name, network.Type)
	}
	if network.Config[ownerKey] != ownerValue || network.Config[resourceKey] != resourceValue {
		return fmt.Errorf("network %q already exists but is not owned by Aginctus", name)
	}
	return nil
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
