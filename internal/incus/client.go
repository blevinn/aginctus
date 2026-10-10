package incus

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

type Server interface {
	GetServer() (*api.Server, string, error)
}

type Connector func(context.Context) (Server, error)

type instanceServer interface {
	GetInstance(name string) (*api.Instance, string, error)
	GetInstanceState(name string) (*api.InstanceState, string, error)
	ExecInstance(instanceName string, exec api.InstanceExecPost, args *incusclient.InstanceExecArgs) (incusclient.Operation, error)
}

type InstanceExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

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

func (c *Client) InstanceConfig(ctx context.Context, name string) (map[string]string, error) {
	server, err := c.connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("connect to local Incus daemon: %w", err)
	}
	instances, ok := server.(instanceServer)
	if !ok {
		return nil, fmt.Errorf("Incus connection does not support instance operations")
	}
	instance, _, err := instances.GetInstance(name)
	if err != nil {
		return nil, fmt.Errorf("get instance %q: %w", name, err)
	}
	config := make(map[string]string, len(instance.Config))
	for key, value := range instance.Config {
		config[key] = value
	}
	return config, nil
}

func (c *Client) ExecInstance(ctx context.Context, name string, command []string, stdin string) (InstanceExecResult, error) {
	if len(command) == 0 {
		return InstanceExecResult{}, fmt.Errorf("instance exec command must not be empty")
	}
	server, err := c.connect(ctx)
	if err != nil {
		return InstanceExecResult{}, fmt.Errorf("connect to local Incus daemon: %w", err)
	}
	instances, ok := server.(instanceServer)
	if !ok {
		return InstanceExecResult{}, fmt.Errorf("Incus connection does not support instance operations")
	}

	var stdout, stderr bytes.Buffer
	dataDone := make(chan bool)
	op, err := instances.ExecInstance(name, api.InstanceExecPost{
		Command:     append([]string(nil), command...),
		WaitForWS:   true,
		Interactive: false,
	}, &incusclient.InstanceExecArgs{
		Stdin:    strings.NewReader(stdin),
		Stdout:   &stdout,
		Stderr:   &stderr,
		DataDone: dataDone,
	})
	if err != nil {
		return InstanceExecResult{}, fmt.Errorf("exec in instance %q: %w", name, err)
	}
	if err := op.WaitContext(ctx); err != nil {
		return InstanceExecResult{}, fmt.Errorf("wait for exec in instance %q: %w", name, err)
	}
	select {
	case <-dataDone:
	case <-ctx.Done():
		return InstanceExecResult{}, ctx.Err()
	}

	exitCode, err := instanceExecExitCode(op.Get().Metadata)
	if err != nil {
		return InstanceExecResult{}, fmt.Errorf("read exec status in instance %q: %w", name, err)
	}
	return InstanceExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}, nil
}

func (c *Client) InstanceAddress(ctx context.Context, name, interfaceName string) (string, error) {
	server, err := c.connect(ctx)
	if err != nil {
		return "", fmt.Errorf("connect to local Incus daemon: %w", err)
	}
	instances, ok := server.(instanceServer)
	if !ok {
		return "", fmt.Errorf("Incus connection does not support instance operations")
	}
	state, _, err := instances.GetInstanceState(name)
	if err != nil {
		return "", fmt.Errorf("get instance %q state: %w", name, err)
	}
	network, ok := state.Network[interfaceName]
	if !ok {
		return "", fmt.Errorf("instance %q has no network interface %q", name, interfaceName)
	}

	for _, family := range []string{"inet", "inet6"} {
		for _, address := range network.Addresses {
			if address.Family != family || address.Scope != "global" {
				continue
			}
			ip := net.ParseIP(address.Address)
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			return address.Address, nil
		}
	}
	return "", fmt.Errorf("instance %q interface %q has no usable global address", name, interfaceName)
}

func instanceExecExitCode(metadata map[string]any) (int, error) {
	if metadata == nil {
		return 0, fmt.Errorf("missing operation metadata")
	}
	value, ok := metadata["return"]
	if !ok {
		return 0, fmt.Errorf("missing return code")
	}
	switch value := value.(type) {
	case float64:
		return int(value), nil
	case int:
		return value, nil
	case int64:
		return int(value), nil
	default:
		return 0, fmt.Errorf("unexpected return code type %T", value)
	}
}
