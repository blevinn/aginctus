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

type MutationOptions struct {
	DryRun bool
	Force  bool
}

type EnsureResult struct {
	Created bool
	Updated bool
	DryRun  bool
}

type TeardownResult struct {
	Deleted bool
	DryRun  bool
}

type Server interface {
	GetServer() (*api.Server, string, error)
	GetNetwork(string) (*api.Network, string, error)
	CreateNetwork(api.NetworksPost) error
	UpdateNetwork(string, api.NetworkPut, string) error
	DeleteNetwork(string) error
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

func (c *Client) EnsureManagementNetwork(ctx context.Context, spec ManagementNetworkSpec, options MutationOptions) (EnsureResult, error) {
	server, err := c.connect(ctx)
	if err != nil {
		return EnsureResult{}, fmt.Errorf("connect to local Incus daemon: %w", err)
	}

	network, etag, err := server.GetNetwork(spec.Name)
	if err == nil {
		if err := validateManagementNetwork(network, spec.Name, options.Force); err != nil {
			return EnsureResult{}, err
		}

		update := network.Writable()
		if update.Config == nil {
			update.Config = api.ConfigMap{}
		}
		changed := applyManagementNetworkConfig(update.Config, spec)
		if !changed {
			return EnsureResult{}, nil
		}
		if options.DryRun {
			return EnsureResult{Updated: true, DryRun: true}, nil
		}
		if err := server.UpdateNetwork(spec.Name, update, etag); err != nil {
			return EnsureResult{}, fmt.Errorf("update management network %q: %w", spec.Name, err)
		}
		return EnsureResult{Updated: true}, nil
	}
	if !api.StatusErrorCheck(err, http.StatusNotFound) {
		return EnsureResult{}, fmt.Errorf("get management network %q: %w", spec.Name, err)
	}

	if options.DryRun {
		return EnsureResult{Created: true, DryRun: true}, nil
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
		return EnsureResult{}, fmt.Errorf("create management network %q: %w", spec.Name, err)
	}

	return EnsureResult{Created: true}, nil
}


func (c *Client) TeardownManagementNetwork(ctx context.Context, name string, options MutationOptions) (TeardownResult, error) {
	server, err := c.connect(ctx)
	if err != nil {
		return TeardownResult{}, fmt.Errorf("connect to local Incus daemon: %w", err)
	}

	network, _, err := server.GetNetwork(name)
	if api.StatusErrorCheck(err, http.StatusNotFound) {
		return TeardownResult{}, nil
	}
	if err != nil {
		return TeardownResult{}, fmt.Errorf("get management network %q: %w", name, err)
	}
	if err := validateManagementNetwork(network, name, options.Force); err != nil {
		return TeardownResult{}, err
	}

	if options.DryRun {
		return TeardownResult{Deleted: true, DryRun: true}, nil
	}
	if err := server.DeleteNetwork(name); err != nil {
		return TeardownResult{}, fmt.Errorf("delete management network %q: %w", name, err)
	}

	return TeardownResult{Deleted: true}, nil
}

func validateManagementNetwork(network *api.Network, name string, force bool) error {
	if !network.Managed {
		return fmt.Errorf("network %q already exists but is not Incus-managed", name)
	}
	if network.Type != "bridge" {
		return fmt.Errorf("network %q already exists with type %q, want bridge", name, network.Type)
	}
	if network.Config[ownerKey] != ownerValue || network.Config[resourceKey] != resourceValue {
		if force {
			return nil
		}
		return fmt.Errorf("network %q already exists but is not owned by Aginctus; use --force to adopt it", name)
	}
	return nil
}

func applyManagementNetworkConfig(config api.ConfigMap, spec ManagementNetworkSpec) bool {
	desired := map[string]string{
		"ipv4.address":  spec.IPv4Address,
		"ipv4.nat":      boolString(spec.IPv4NAT),
		"ipv4.routing":  boolString(spec.IPv4Routing),
		"ipv6.address":  spec.IPv6Address,
		ownerKey:        ownerValue,
		resourceKey:     resourceValue,
	}

	changed := false
	for key, value := range desired {
		if config[key] != value {
			config[key] = value
			changed = true
		}
	}
	return changed
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
