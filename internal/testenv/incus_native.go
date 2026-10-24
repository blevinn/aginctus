package testenv

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

// nativeIncusVMClient is a project-scoped SDK adapter, constructed only by
// the trusted, installed worker. No Incus socket or URL is supplied by MCP.
// The daemon socket is effectively host-admin authority and MUST NOT be
// given to a coding agent or a regular user session.
type nativeIncusVMClient struct {
	server incusclient.InstanceServer
}

// ConnectTrustedIncusVMClient is for installed worker configuration ONLY;
// the public testenv CLI/MCP does not call it. Do not use Incus default
// socket resolution or inherited INCUS_SOCKET/INCUS_DIR variables.
func ConnectTrustedIncusVMClient(ctx context.Context, socketPath string) (IncusVMClient, error) {
	if !filepath.IsAbs(socketPath) || filepath.Clean(socketPath) != socketPath {
		return nil, errors.New("trusted Incus socket path must be absolute and clean")
	}
	info, err := os.Lstat(socketPath)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("trusted Incus socket unavailable or unsafe")
	}
	// Incus may be installed under a root-owned parent. Configuration of
	// this path is a trusted worker deployment responsibility.
	server, err := incusclient.ConnectIncusUnixWithContext(ctx, socketPath, &incusclient.ConnectionArgs{SkipGetServer: true})
	if err != nil {
		return nil, errors.New("could not connect to configured Incus worker daemon")
	}
	return &nativeIncusVMClient{server: server.UseProject(DefaultVMProject)}, nil
}

func (n *nativeIncusVMClient) ProjectConfig(ctx context.Context) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, _, err := n.server.GetProject(DefaultVMProject)
	if err != nil {
		return nil, err
	}
	config := make(map[string]string, len(project.Config))
	for k, v := range project.Config {
		config[k] = v
	}
	return config, nil
}

func (n *nativeIncusVMClient) HasImage(ctx context.Context, fingerprint string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !vmDigest.MatchString(fingerprint) {
		return false, errors.New("invalid immutable image fingerprint")
	}
	image, _, err := n.server.GetImage(fingerprint)
	if api.StatusErrorCheck(err, http.StatusNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return image != nil && image.Fingerprint == fingerprint, nil
}

func fromIncusInstance(instance api.Instance) IncusVMInstance {
	devices := make(map[string]map[string]string, len(instance.ExpandedDevices))
	for name, settings := range instance.ExpandedDevices {
		copySettings := make(map[string]string, len(settings))
		for k, v := range settings {
			copySettings[k] = v
		}
		devices[name] = copySettings
	}
	config := make(map[string]string, len(instance.ExpandedConfig))
	for k, v := range instance.ExpandedConfig {
		config[k] = v
	}
	return IncusVMInstance{
		Name: instance.Name, Type: string(instance.Type),
		Status: instance.Status, Ephemeral: instance.Ephemeral,
		Profiles: append([]string{}, instance.Profiles...),
		Config: config, Devices: devices,
	}
}

func (n *nativeIncusVMClient) Get(ctx context.Context, name string) (*IncusVMInstance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	instance, _, err := n.server.GetInstance(name)
	if api.StatusErrorCheck(err, http.StatusNotFound) {
		return nil, ErrIncusInstanceMissing
	}
	if err != nil {
		return nil, err
	}
	result := fromIncusInstance(*instance)
	return &result, nil
}

func (n *nativeIncusVMClient) Create(ctx context.Context, plan IncusInstancePlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if plan.Project != DefaultVMProject || !vmDigest.MatchString(plan.ImageFingerprint) ||
		!vmSafeName.MatchString(plan.Name) || plan.Type != "virtual-machine" ||
		len(plan.Profiles) != 0 || len(plan.Devices) != 1 ||
		plan.Devices["root"]["type"] != "disk" || plan.Devices["root"]["source"] != "" {
		return errors.New("Incus create plan violated trusted policy")
	}
	req := api.InstancesPost{
		Name: plan.Name, Type: api.InstanceTypeVM,
		Source: api.InstanceSource{Type: "image", Fingerprint: plan.ImageFingerprint},
		InstancePut: api.InstancePut{
			Config: api.ConfigMap(plan.Config),
			Devices: api.DevicesMap(plan.Devices),
			Profiles: []string{},
			Ephemeral: plan.Ephemeral,
		},
	}
	op, err := n.server.CreateInstance(req)
	if err != nil {
		return err
	}
	return op.WaitContext(ctx)
}

func (n *nativeIncusVMClient) Start(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	op, err := n.server.UpdateInstanceState(name, api.InstanceStatePut{
		Action: "start", Timeout: 30,
	}, "")
	if err != nil {
		return err
	}
	return op.WaitContext(ctx)
}

func (n *nativeIncusVMClient) Stop(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	op, err := n.server.UpdateInstanceState(name, api.InstanceStatePut{
		Action: "stop", Force: true, Timeout: 30,
	}, "")
	if err != nil {
		return err
	}
	return op.WaitContext(ctx)
}

func (n *nativeIncusVMClient) Delete(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	op, err := n.server.DeleteInstance(name)
	if err != nil {
		return err
	}
	return op.WaitContext(ctx)
}

func (n *nativeIncusVMClient) List(ctx context.Context) ([]IncusVMInstance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	instances, err := n.server.GetInstances(api.InstanceTypeVM)
	if err != nil {
		return nil, err
	}
	result := make([]IncusVMInstance, 0, len(instances))
	for _, instance := range instances {
		if strings.TrimSpace(instance.Name) == "" {
			return nil, errors.New("invalid Incus instance response")
		}
		result = append(result, fromIncusInstance(instance))
	}
	return result, nil
}
