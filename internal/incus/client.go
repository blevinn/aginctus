package incus

import (
	"context"
	"fmt"
	"net/http"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

const (
	ownerKey       = "user.aginctus.managed"
	resourceKey    = "user.aginctus.resource"
	roleKey        = "user.aginctus.role"
	ownerValue     = "true"
	resourceValue  = "management-network"
	herdrRoleValue = "herdr-client"
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

type operation interface {
	Wait() error
}

type Server interface {
	GetServer() (*api.Server, string, error)
	GetNetwork(string) (*api.Network, string, error)
	CreateNetwork(api.NetworksPost) error
	UpdateNetwork(string, api.NetworkPut, string) error
	DeleteNetwork(string) error
	GetInstance(string) (*api.Instance, string, error)
	CreateInstanceFromLocalImage(string, api.InstancesPost) (operation, error)
	UpdateInstance(string, api.InstancePut, string) (operation, error)
	DeleteInstance(string) (operation, error)
	UpdateInstanceState(string, api.InstanceStatePut, string) (operation, error)
}

type realServer struct {
	incusclient.InstanceServer
}

func (s realServer) CreateInstanceFromLocalImage(alias string, instance api.InstancesPost) (operation, error) {
	imageAlias, _, err := s.InstanceServer.GetImageAlias(alias)
	if err != nil {
		return nil, fmt.Errorf("get local image alias %q: %w", alias, err)
	}

	image, _, err := s.InstanceServer.GetImage(imageAlias.Target)
	if err != nil {
		return nil, fmt.Errorf("get local image %q: %w", imageAlias.Target, err)
	}

	return s.InstanceServer.CreateInstanceFromImage(s.InstanceServer, *image, instance)
}

func (s realServer) UpdateInstance(name string, instance api.InstancePut, etag string) (operation, error) {
	return s.InstanceServer.UpdateInstance(name, instance, etag)
}

func (s realServer) DeleteInstance(name string) (operation, error) {
	return s.InstanceServer.DeleteInstance(name)
}

func (s realServer) UpdateInstanceState(name string, state api.InstanceStatePut, etag string) (operation, error) {
	return s.InstanceServer.UpdateInstanceState(name, state, etag)
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
		return realServer{InstanceServer: server}, nil
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
				"ipv4.nat":     boolString(spec.IPv4NAT),
				"ipv4.routing": boolString(spec.IPv4Routing),
				"ipv6.address": spec.IPv6Address,
				ownerKey:       ownerValue,
				resourceKey:    resourceValue,
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
		"ipv4.address": spec.IPv4Address,
		"ipv4.nat":     boolString(spec.IPv4NAT),
		"ipv4.routing": boolString(spec.IPv4Routing),
		"ipv6.address": spec.IPv6Address,
		ownerKey:       ownerValue,
		resourceKey:    resourceValue,
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

type HerdrClientSpec struct {
	Name              string
	ImageAlias        string
	StoragePool       string
	ManagementNetwork string
}

func (c *Client) EnsureHerdrClient(ctx context.Context, spec HerdrClientSpec, options MutationOptions) (EnsureResult, error) {
	server, err := c.connect(ctx)
	if err != nil {
		return EnsureResult{}, fmt.Errorf("connect to local Incus daemon: %w", err)
	}

	instance, etag, err := server.GetInstance(spec.Name)
	if err == nil {
		if err := validateHerdrClient(instance, spec.Name, options.Force); err != nil {
			return EnsureResult{}, err
		}

		update := instance.Writable()
		if update.Config == nil {
			update.Config = api.ConfigMap{}
		}
		if update.Devices == nil {
			update.Devices = api.DevicesMap{}
		}

		changed := applyHerdrClientConfig(&update, spec)
		needsStart := instance.Status != "Running"
		if !changed && !needsStart {
			return EnsureResult{}, nil
		}
		if options.DryRun {
			return EnsureResult{Updated: true, DryRun: true}, nil
		}

		if changed {
			op, err := server.UpdateInstance(spec.Name, update, etag)
			if err != nil {
				return EnsureResult{}, fmt.Errorf("update Herdr client %q: %w", spec.Name, err)
			}
			if err := op.Wait(); err != nil {
				return EnsureResult{}, fmt.Errorf("wait for Herdr client %q update: %w", spec.Name, err)
			}
		}
		if needsStart {
			op, err := server.UpdateInstanceState(spec.Name, api.InstanceStatePut{Action: "start", Timeout: -1}, "")
			if err != nil {
				return EnsureResult{}, fmt.Errorf("start Herdr client %q: %w", spec.Name, err)
			}
			if err := op.Wait(); err != nil {
				return EnsureResult{}, fmt.Errorf("wait for Herdr client %q start: %w", spec.Name, err)
			}
		}
		return EnsureResult{Updated: true}, nil
	}
	if !api.StatusErrorCheck(err, http.StatusNotFound) {
		return EnsureResult{}, fmt.Errorf("get Herdr client %q: %w", spec.Name, err)
	}

	if options.DryRun {
		return EnsureResult{Created: true, DryRun: true}, nil
	}

	request := api.InstancesPost{
		Name:  spec.Name,
		Type:  api.InstanceTypeContainer,
		Start: true,

		InstancePut: api.InstancePut{
			Description: "Aginctus Herdr client infrastructure",
			Profiles:    []string{},
			Config: api.ConfigMap{
				ownerKey:    ownerValue,
				resourceKey: "infrastructure",
				roleKey:     herdrRoleValue,
			},
			Devices: api.DevicesMap{
				"root": {
					"type": "disk",
					"path": "/",
					"pool": spec.StoragePool,
				},
				"management": {
					"type":    "nic",
					"network": spec.ManagementNetwork,
					"name":    "eth0",
				},
			},
		},
	}

	op, err := server.CreateInstanceFromLocalImage(spec.ImageAlias, request)
	if err != nil {
		return EnsureResult{}, fmt.Errorf("create Herdr client %q from image %q: %w", spec.Name, spec.ImageAlias, err)
	}
	if err := op.Wait(); err != nil {
		return EnsureResult{}, fmt.Errorf("wait for Herdr client %q creation: %w", spec.Name, err)
	}

	return EnsureResult{Created: true}, nil
}

func (c *Client) TeardownHerdrClient(ctx context.Context, name string, options MutationOptions) (TeardownResult, error) {
	server, err := c.connect(ctx)
	if err != nil {
		return TeardownResult{}, fmt.Errorf("connect to local Incus daemon: %w", err)
	}

	instance, _, err := server.GetInstance(name)
	if api.StatusErrorCheck(err, http.StatusNotFound) {
		return TeardownResult{}, nil
	}
	if err != nil {
		return TeardownResult{}, fmt.Errorf("get Herdr client %q: %w", name, err)
	}
	if err := validateHerdrClient(instance, name, options.Force); err != nil {
		return TeardownResult{}, err
	}

	if options.DryRun {
		return TeardownResult{Deleted: true, DryRun: true}, nil
	}

	if instance.Status != "Stopped" {
		op, err := server.UpdateInstanceState(name, api.InstanceStatePut{
			Action:  "stop",
			Timeout: -1,
			Force:   options.Force,
		}, "")
		if err != nil {
			return TeardownResult{}, fmt.Errorf("stop Herdr client %q: %w", name, err)
		}
		if err := op.Wait(); err != nil {
			return TeardownResult{}, fmt.Errorf("wait for Herdr client %q stop: %w", name, err)
		}
	}

	op, err := server.DeleteInstance(name)
	if err != nil {
		return TeardownResult{}, fmt.Errorf("delete Herdr client %q: %w", name, err)
	}
	if err := op.Wait(); err != nil {
		return TeardownResult{}, fmt.Errorf("wait for Herdr client %q deletion: %w", name, err)
	}

	return TeardownResult{Deleted: true}, nil
}

func validateHerdrClient(instance *api.Instance, name string, force bool) error {
	if instance.Type != string(api.InstanceTypeContainer) {
		return fmt.Errorf("instance %q already exists with type %q, want container", name, instance.Type)
	}
	if instance.Config[ownerKey] != ownerValue || instance.Config[resourceKey] != "infrastructure" || instance.Config[roleKey] != herdrRoleValue {
		if force {
			return nil
		}
		return fmt.Errorf("instance %q already exists but is not the Aginctus Herdr client; use --force to adopt it", name)
	}
	return nil
}

func applyHerdrClientConfig(instance *api.InstancePut, spec HerdrClientSpec) bool {
	changed := false
	desiredConfig := map[string]string{
		ownerKey:    ownerValue,
		resourceKey: "infrastructure",
		roleKey:     herdrRoleValue,
	}
	for key, value := range desiredConfig {
		if instance.Config[key] != value {
			instance.Config[key] = value
			changed = true
		}
	}

	desiredDevices := api.DevicesMap{
		"root": {
			"type": "disk",
			"path": "/",
			"pool": spec.StoragePool,
		},
		"management": {
			"type":    "nic",
			"network": spec.ManagementNetwork,
			"name":    "eth0",
		},
	}
	for name, desired := range desiredDevices {
		current, ok := instance.Devices[name]
		if !ok || !stringMapEqual(current, desired) {
			instance.Devices[name] = desired
			changed = true
		}
	}

	return changed
}

func stringMapEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
