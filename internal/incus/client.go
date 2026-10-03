package incus

import (
	"context"
	"fmt"
	"net/http"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

const (
	ManagementNetworkName = "aginctus-mgmt"

	ownerKey    = "user.aginctus.managed"
	resourceKey = "user.aginctus.resource"

	ownerValue    = "true"
	resourceValue = "management-network"
)

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
	return &Client{
		connect: func(ctx context.Context) (Server, error) {
			return incusclient.ConnectIncusUnixWithContext(
				ctx,
				"",
				&incusclient.ConnectionArgs{SkipGetServer: true},
			)
		},
	}
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

func (c *Client) EnsureManagementNetwork(ctx context.Context) (bool, error) {
	server, err := c.connect(ctx)
	if err != nil {
		return false, fmt.Errorf("connect to local Incus daemon: %w", err)
	}

	network, _, err := server.GetNetwork(ManagementNetworkName)
	if err == nil {
		if err := validateManagementNetwork(network); err != nil {
			return false, err
		}
		return false, nil
	}
	if !api.StatusErrorCheck(err, http.StatusNotFound) {
		return false, fmt.Errorf("get management network %q: %w", ManagementNetworkName, err)
	}

	err = server.CreateNetwork(api.NetworksPost{
		Name: ManagementNetworkName,
		Type: "bridge",
		NetworkPut: api.NetworkPut{
			Description: "Aginctus management network",
			Config: api.ConfigMap{
				"ipv4.address":  "auto",
				"ipv4.nat":      "false",
				"ipv6.address":  "none",
				ownerKey:        ownerValue,
				resourceKey:     resourceValue,
			},
		},
	})
	if err != nil {
		return false, fmt.Errorf("create management network %q: %w", ManagementNetworkName, err)
	}

	return true, nil
}

func validateManagementNetwork(network *api.Network) error {
	if !network.Managed {
		return fmt.Errorf("network %q already exists but is not Incus-managed", ManagementNetworkName)
	}
	if network.Type != "bridge" {
		return fmt.Errorf("network %q already exists with type %q, want bridge", ManagementNetworkName, network.Type)
	}
	if network.Config[ownerKey] != ownerValue || network.Config[resourceKey] != resourceValue {
		return fmt.Errorf("network %q already exists but is not owned by Aginctus", ManagementNetworkName)
	}
	return nil
}
