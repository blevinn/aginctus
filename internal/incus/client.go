package incus

import (
	"context"
	"fmt"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

type Server interface {
	GetServer() (*api.Server, string, error)
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
