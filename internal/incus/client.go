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
	return &Client{connect: func(ctx context.Context) (Server, error) {
		server, err := incusclient.ConnectIncusUnixWithContext(ctx, "", &incusclient.ConnectionArgs{SkipGetServer: true})
		if err != nil {
			return nil, err
		}
		return server, nil
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

func (c *Client) HTTPSAddress(ctx context.Context) (string, error) {
	server, err := c.connect(ctx)
	if err != nil {
		return "", fmt.Errorf("connect to local Incus daemon: %w", err)
	}
	status, _, err := server.GetServer()
	if err != nil {
		return "", fmt.Errorf("get Incus server configuration: %w", err)
	}
	return status.Config["core.https_address"], nil
}

// SetHTTPSAddress remediates only an absent listener on the local SDK
// connection. Recheck on the same connection used for the conditional write.
func (c *Client) SetHTTPSAddress(ctx context.Context, address string) error {
	if address != "127.0.0.1:8443" {
		return fmt.Errorf("unsupported HTTPS listener remediation address")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	server, err := c.connect(ctx)
	if err != nil {
		return fmt.Errorf("connect to local Incus daemon: %w", err)
	}
	if disconnect, ok := server.(interface{ Disconnect() }); ok {
		defer disconnect.Disconnect()
	}
	writer, ok := server.(interface {
		UpdateServer(api.ServerPut, string) error
	})
	if !ok {
		return fmt.Errorf("incus connection cannot update server configuration")
	}
	status, etag, err := server.GetServer()
	if err != nil {
		return fmt.Errorf("get Incus server configuration: %w", err)
	}
	if status == nil || etag == "" || status.Config["core.https_address"] != "" {
		return fmt.Errorf("incus listener absence or revision unverified")
	}
	put := status.Writable()
	if put.Config == nil {
		put.Config = api.ConfigMap{}
	}
	put.Config["core.https_address"] = address
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := writer.UpdateServer(put, etag); err != nil {
		return fmt.Errorf("update local Incus listener: %w", err)
	}
	after, _, err := server.GetServer()
	if err != nil || after == nil || after.Config["core.https_address"] != address {
		return fmt.Errorf("incus listener update outcome unverified")
	}
	return nil
}
