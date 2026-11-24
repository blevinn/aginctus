package gateway

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
)

var gatewayServerFingerprint = regexp.MustCompile(`^[0-9a-f]{64}$`)

// GatewayVolumeResult distinguishes an observed no-op from a verified create.
type GatewayVolumeResult struct {
	Project       string `json:"project"`
	EnvironmentID string `json:"environmentId"`
	Pool          string `json:"pool"`
	Name          string `json:"name"`
	Created       bool   `json:"created"`
	Verified      bool   `json:"verified"`
}

// GatewayVolumeError deliberately does not include daemon error bodies. Once
// Create was attempted, callers must inspect the volume before retrying.
type GatewayVolumeError struct {
	Stage     string
	Attempted bool
	Uncertain bool
	cause     error
}

func (e *GatewayVolumeError) Error() string {
	return "native gateway volume failed at " + e.Stage + "; inspect the selected volume before retry; no automatic cleanup"
}

func (e *GatewayVolumeError) Unwrap() error { return e.cause }

func gatewayVolumeMarkers(project, environmentID string) map[string]string {
	return map[string]string{
		"user.aginctus.managed":        "true",
		"user.aginctus.resource":       "gateway-postgres-volume",
		"user.aginctus.deployment":     project,
		"user.aginctus.environment-id": environmentID,
		"security.shifted":             "true",
	}
}

// GatewayVolumeClient exposes only reads and creation; callers cannot update,
// attach, rename, or delete through this interface.
type GatewayVolumeClient interface {
	GetConnectionInfo() (*incus.ConnectionInfo, error)
	GetServer() (*api.Server, string, error)
	GetProject(string) (*api.Project, string, error)
	GetStoragePoolVolume(pool, volumeType, name string) (*api.StorageVolume, string, error)
	CreateStoragePoolVolume(pool string, request api.StorageVolumesPost) error
}

// EnsureNativeEnvironmentGatewayVolumeWithClient creates or verifies one
// environment's PostgreSQL filesystem volume in an administrator-selected pool.
// The selected binding must have been independently verified; the same local
// Unix client must report that server certificate fingerprint and project.
// The caller must pin accepted state and hold exclusive writer authority;
// guard must recheck that authority at the create boundary. Neither the
// binding nor the volume's markers grant authority to create or attach it.
func EnsureNativeEnvironmentGatewayVolumeWithClient(ctx context.Context, descriptor environment.Descriptor, selected environment.Binding, pool string, client GatewayVolumeClient, guard func(context.Context) error) (GatewayVolumeResult, error) {
	result := GatewayVolumeResult{}
	fail := func(stage string, attempted bool, cause error) (GatewayVolumeResult, error) {
		return result, &GatewayVolumeError{Stage: stage, Attempted: attempted, Uncertain: attempted, cause: cause}
	}
	if guard == nil {
		return fail("authority", false, fmt.Errorf("gateway volume creation requires an authority guard"))
	}
	identity, err := NativeGatewayIdentity(descriptor, selected)
	if err != nil {
		return fail("environment", false, err)
	}
	if !nativeDeviceName.MatchString(pool) {
		return fail("intent", false, fmt.Errorf("gateway volume requires an explicit safe pool name"))
	}
	result = GatewayVolumeResult{Project: identity.Project, EnvironmentID: identity.EnvironmentID, Pool: pool, Name: identity.DatabaseVolume}
	if err := ctx.Err(); err != nil {
		return fail("cancellation", false, err)
	}
	scope, err := incusscope.NewProjectScope(identity.Project)
	if err != nil {
		return fail("binding", false, err)
	}
	checkBinding := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		connection, err := client.GetConnectionInfo()
		if err != nil || connection == nil || connection.Protocol != "incus" ||
			!filepath.IsAbs(connection.SocketPath) || connection.Project != scope.Name || connection.Target != "" {
			return fmt.Errorf("local selected-project transport unavailable")
		}
		server, _, err := client.GetServer()
		if err != nil || server == nil || !gatewayServerFingerprint.MatchString(server.Environment.CertificateFingerprint) ||
			"sha256:"+server.Environment.CertificateFingerprint != selected.ServerID || server.Environment.Project != scope.Name {
			return fmt.Errorf("selected Incus server/project binding unverified")
		}
		p, _, err := client.GetProject(scope.Name)
		if err != nil || p == nil || scope.AssessProject(p.Name, p.Config) != incusscope.ProjectCompatible {
			return fmt.Errorf("selected project ownership or policy unverified")
		}
		return ctx.Err()
	}
	inspect := func() (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		volume, _, err := client.GetStoragePoolVolume(pool, "custom", identity.DatabaseVolume)
		if api.StatusErrorCheck(err, http.StatusNotFound) {
			return false, ctx.Err()
		}
		if err != nil || volume == nil {
			return false, fmt.Errorf("selected volume observation unavailable")
		}
		if err := verifyGatewayVolume(volume, identity.Project, identity.EnvironmentID, identity.DatabaseVolume, identity.PostgresInstance); err != nil {
			return false, err
		}
		return true, ctx.Err()
	}
	if err := checkBinding(); err != nil {
		return fail("binding", false, err)
	}
	exists, err := inspect()
	if err != nil {
		return fail("preflight", false, err)
	}
	if exists {
		result.Verified = true
		return result, nil
	}
	if err := guard(ctx); err != nil {
		return fail("authority", false, err)
	}
	if err := checkBinding(); err != nil {
		return fail("mutation-boundary", false, err)
	}
	exists, err = inspect()
	if err != nil {
		return fail("mutation-boundary", false, err)
	}
	if exists {
		return fail("mutation-boundary", false, fmt.Errorf("volume absence not verified at mutation boundary"))
	}
	if err := ctx.Err(); err != nil {
		return fail("cancellation", false, err)
	}
	request := api.StorageVolumesPost{
		Name: identity.DatabaseVolume, Type: "custom", ContentType: "filesystem",
		StorageVolumePut: api.StorageVolumePut{
			Description: "Aginctus managed gateway PostgreSQL data",
			Config:      gatewayVolumeMarkers(identity.Project, identity.EnvironmentID),
		},
	}
	if err := client.CreateStoragePoolVolume(pool, request); err != nil {
		return fail("create", true, err)
	}
	result.Created = true
	if err := checkBinding(); err != nil {
		return fail("verification", true, err)
	}
	exists, err = inspect()
	if err != nil {
		return fail("verification", true, err)
	}
	if !exists {
		return fail("verification", true, fmt.Errorf("created volume ownership or identity unverified"))
	}
	result.Verified = true
	return result, nil
}
