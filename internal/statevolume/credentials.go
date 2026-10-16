package statevolume

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

// GatewayCredentials describes dedicated credential-manager-owned
// gateway state storage. When explicitly provisioned for native ChatGPT,
// only the LiteLLM instance may receive the private volume. It must never
// be attached to PostgreSQL, the Herdr client, or workloads.
type GatewayCredentials struct {
	Pool      string
	GatewayID string
	Project   string // Empty selects the legacy default Incus project.
}

var gatewayID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

func (v GatewayCredentials) Validate() error {
	if !safeName.MatchString(v.Pool) || !gatewayID.MatchString(v.GatewayID) || (v.Project != "" && !safeName.MatchString(v.Project)) {
		return fmt.Errorf("unsafe gateway credential volume pool or gateway ID")
	}
	return nil
}

func (v GatewayCredentials) Name() string {
	return "aginctus-gateway-" + v.GatewayID + "-credentials"
}

func (v GatewayCredentials) markers() map[string]string {
	return map[string]string{
		"user.aginctus.managed":  "true",
		"user.aginctus.resource": "gateway-credentials",
		"user.aginctus.gateway":  v.GatewayID,
	}
}

func VerifyGatewayCredentials(v GatewayCredentials, got *api.StorageVolume) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if got == nil || got.Name != v.Name() || got.Type != "custom" || got.ContentType != "filesystem" {
		return fmt.Errorf("gateway credential volume %q has unexpected name, type, or content", v.Name())
	}
	for key, expected := range v.markers() {
		if got.Config[key] != expected {
			return fmt.Errorf("gateway credential volume %q ownership mismatch at %s", v.Name(), key)
		}
	}
	return nil
}

// EnsureGatewayCredentials creates only missing owned storage. Existing foreign
// names fail closed; retained volumes are never erased or adopted.
func EnsureGatewayCredentials(ctx context.Context, backend Backend, v GatewayCredentials) error {
	if err := v.Validate(); err != nil {
		return err
	}
	got, err := backend.Get(ctx, v.Pool, v.Name())
	if err == nil {
		return VerifyGatewayCredentials(v, got)
	}
	if _, ok := err.(NotFound); !ok {
		return fmt.Errorf("inspect gateway credential storage: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	req := api.StorageVolumesPost{
		Name: v.Name(), Type: "custom", ContentType: "filesystem",
		StorageVolumePut: api.StorageVolumePut{
			Description: "Aginctus gateway credential-manager state (no direct workload access)",
			Config:      v.markers(),
		},
	}
	if err := backend.Create(ctx, v.Pool, req); err != nil {
		return fmt.Errorf("create gateway credential volume %q: %w", v.Name(), err)
	}
	got, err = backend.Get(ctx, v.Pool, v.Name())
	if err != nil {
		return fmt.Errorf("verify created gateway credential volume: %w", err)
	}
	return VerifyGatewayCredentials(v, got)
}

// EnsureGatewayCredentialsOnHost uses the trusted local Incus socket. It does
// not mount or expose credentials, and is safe to repeat.
func EnsureGatewayCredentialsOnHost(ctx context.Context, v GatewayCredentials) error {
	if err := v.Validate(); err != nil {
		return err
	}
	server, err := incusclient.ConnectIncusUnixWithContext(ctx, "", &incusclient.ConnectionArgs{SkipGetServer: true})
	if err != nil {
		return fmt.Errorf("connect local Incus for gateway credential storage: %w", err)
	}
	project := v.Project
	if project == "" {
		project = Project
	}
	server = server.UseProject(project)
	// Do not permit a similarly named foreign project/resource to become
	// the destination for secrets once the gateway service is introduced.
	_, _, err = server.GetStoragePoolVolume(v.Pool, "custom", v.Name())
	if err != nil && !api.StatusErrorCheck(err, http.StatusNotFound) {
		return fmt.Errorf("inspect candidate credential storage: %w", err)
	}
	return EnsureGatewayCredentials(ctx, nativeBackend{server: server}, v)
}
