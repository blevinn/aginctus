package testenv

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

// nativeIncusVMClient is a project-scoped SDK adapter, constructed only by
// the trusted, installed worker. No Incus socket or URL is supplied by MCP.
// The daemon socket is effectively host-admin authority and MUST NOT be
// given to a coding agent or a regular user session.
type nativeIncusVMClient struct {
	server *incusclient.ProtocolIncus
}

const incusRequestTimeout = 30 * time.Second

func waitIncusOperation(ctx context.Context, op incusclient.Operation) error {
	// HTTP polling avoids SDK event websocket setup without call contexts and
	// GetOperationWait's mutation of the shared HTTP transport timeout.
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if op.Get().StatusCode.IsFinal() {
			if op.Get().StatusCode != api.Success {
				return errors.New("testenv Incus operation did not complete successfully")
			}
			return nil
		}
		if err := op.Refresh(); err != nil {
			return err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// UseProject constructs a new ProtocolIncus, including private event state.
// WithContext mutates its receiver, so it must only be used on that copy.
func (n *nativeIncusVMClient) requestClient(ctx context.Context) (context.Context, incusclient.InstanceServer, context.CancelFunc) {
	requestCtx, cancel := context.WithTimeout(ctx, incusRequestTimeout)
	copy := n.server.UseProject(DefaultVMProject).(*incusclient.ProtocolIncus)
	return requestCtx, copy.WithContext(requestCtx), cancel
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
	server, err := incusclient.ConnectIncusUnixWithContext(ctx, socketPath, &incusclient.ConnectionArgs{SkipGetServer: true, SkipGetEvents: true})
	if err != nil {
		return nil, safeIncusError("could not connect to configured Incus worker daemon", err)
	}
	protocol, ok := server.(*incusclient.ProtocolIncus)
	if !ok {
		return nil, errors.New("unsupported trusted Incus SDK client")
	}
	return &nativeIncusVMClient{server: protocol}, nil
}

func (n *nativeIncusVMClient) ProjectConfig(ctx context.Context) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, server, cancel := n.requestClient(ctx)
	defer cancel()
	project, _, err := server.GetProject(DefaultVMProject)
	if err != nil {
		return nil, safeIncusError("testenv Incus project lookup failed", err)
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
	_, server, cancel := n.requestClient(ctx)
	defer cancel()
	image, _, err := server.GetImage(fingerprint)
	if api.StatusErrorCheck(err, http.StatusNotFound) {
		return false, nil
	}
	if err != nil {
		return false, safeIncusError("testenv Incus image lookup failed", err)
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
		Config:   config, Devices: devices,
	}
}

func (n *nativeIncusVMClient) Get(ctx context.Context, name string) (*IncusVMInstance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, server, cancel := n.requestClient(ctx)
	defer cancel()
	instance, _, err := server.GetInstance(name)
	if api.StatusErrorCheck(err, http.StatusNotFound) {
		return nil, ErrIncusInstanceMissing
	}
	if err != nil {
		return nil, safeIncusError("testenv Incus instance lookup failed", err)
	}
	if instance == nil {
		return nil, errors.New("testenv Incus returned no instance")
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
		return errors.New("testenv Incus create plan violated trusted policy")
	}
	req := api.InstancesPost{
		Name: plan.Name, Type: api.InstanceTypeVM,
		Source: api.InstanceSource{Type: "image", Fingerprint: plan.ImageFingerprint},
		InstancePut: api.InstancePut{
			Config:    api.ConfigMap(plan.Config),
			Devices:   api.DevicesMap(plan.Devices),
			Profiles:  []string{},
			Ephemeral: plan.Ephemeral,
		},
	}
	ctx, server, cancel := n.requestClient(ctx)
	defer cancel()
	op, err := server.CreateInstance(req)
	if err != nil {
		return safeIncusError("testenv Incus create submission failed", err)
	}
	if err := waitIncusOperation(ctx, op); err != nil {
		return safeIncusError("testenv Incus create completion unverified", err)
	}
	return nil
}

func (n *nativeIncusVMClient) Start(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, server, cancel := n.requestClient(ctx)
	defer cancel()
	op, err := server.UpdateInstanceState(name, api.InstanceStatePut{
		Action: "start", Timeout: 30,
	}, "")
	if err != nil {
		return safeIncusError("testenv Incus start submission failed", err)
	}
	if err := waitIncusOperation(ctx, op); err != nil {
		return safeIncusError("testenv Incus start completion unverified", err)
	}
	return nil
}

func (n *nativeIncusVMClient) Stop(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, server, cancel := n.requestClient(ctx)
	defer cancel()
	op, err := server.UpdateInstanceState(name, api.InstanceStatePut{
		Action: "stop", Force: true, Timeout: 30,
	}, "")
	if err != nil {
		if api.StatusErrorCheck(err, http.StatusNotFound) {
			return ErrIncusInstanceMissing
		}
		return safeIncusError("testenv Incus stop submission failed", err)
	}
	if err := waitIncusOperation(ctx, op); err != nil {
		return safeIncusError("testenv Incus stop completion unverified", err)
	}
	return nil
}

func (n *nativeIncusVMClient) Delete(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, server, cancel := n.requestClient(ctx)
	defer cancel()
	op, err := server.DeleteInstance(name)
	if err != nil {
		if api.StatusErrorCheck(err, http.StatusNotFound) {
			return ErrIncusInstanceMissing
		}
		return safeIncusError("testenv Incus delete submission failed", err)
	}
	if err := waitIncusOperation(ctx, op); err != nil {
		return safeIncusError("testenv Incus delete completion unverified", err)
	}
	return nil
}

func (n *nativeIncusVMClient) List(ctx context.Context) ([]IncusVMInstance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, server, cancel := n.requestClient(ctx)
	defer cancel()
	instances, err := server.GetInstances(api.InstanceTypeVM)
	if err != nil {
		return nil, safeIncusError("testenv Incus list failed", err)
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

// RootVolumePresent independently queries the project-scoped VM volume API.
// It does not infer volume reclamation from instance absence or delete volumes.
func (n *nativeIncusVMClient) RootVolumePresent(ctx context.Context, pool, name string) (bool, error) {
	if pool != "testenv" || !vmSafeName.MatchString(name) {
		return false, ErrOwnershipMismatch
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	requestCtx, server, cancel := n.requestClient(ctx)
	defer cancel()
	volume, _, err := server.GetStoragePoolVolume(pool, "virtual-machine", name)
	if ctxErr := requestCtx.Err(); ctxErr != nil {
		return false, safeIncusError("testenv root volume inventory unverified", ctxErr)
	}
	if api.StatusErrorCheck(err, http.StatusNotFound) {
		return false, nil
	}
	if err != nil {
		return false, safeIncusError("testenv root volume inventory unavailable", err)
	}
	if volume == nil || volume.Name != name || volume.Type != "virtual-machine" {
		return false, ErrOwnershipMismatch
	}
	return true, nil
}
