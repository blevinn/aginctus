package native

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// GatewayVolumeIntent describes one project-owned, persistent PostgreSQL data
// volume. The storage pool must be selected by an administrator. Neither
// creation nor inspection authorizes attaching it to a guest.
type GatewayVolumeIntent struct {
	Project string
	Pool string
	Name string
}

// GatewayVolumeResult distinguishes an observed no-op from a verified create.
type GatewayVolumeResult struct {
	Project string `json:"project"`
	Pool string `json:"pool"`
	Name string `json:"name"`
	Created bool `json:"created"`
	Verified bool `json:"verified"`
}

// GatewayVolumeError deliberately does not include daemon error bodies. Once
// Create was attempted, callers must inspect the volume before retrying.
type GatewayVolumeError struct {
	Stage string
	Attempted bool
	Uncertain bool
	cause error
}

func (e *GatewayVolumeError) Error() string {
	return "native gateway volume failed at " + e.Stage + "; inspect the selected volume before retry; no automatic cleanup"
}

func (e *GatewayVolumeError) Unwrap() error { return e.cause }

func (v GatewayVolumeIntent) Validate() error {
	if _, err := incusscope.NewProjectScope(v.Project); err != nil {
		return fmt.Errorf("gateway volume requires explicit nondefault project")
	}
	if !localName(v.Pool) || !localName(v.Name) {
		return fmt.Errorf("gateway volume requires explicit safe pool and name")
	}
	return nil
}

func (v GatewayVolumeIntent) markers() map[string]string {
	return map[string]string{
		"user.aginctus.managed": "true",
		"user.aginctus.resource": "gateway-postgres-volume",
		"user.aginctus.deployment": v.Project,
		"security.shifted": "true",
	}
}

// GatewayVolumeClient exposes only reads and creation; callers cannot update,
// attach, rename, or delete through this interface.
type GatewayVolumeClient interface {
	GetConnectionInfo() (*incus.ConnectionInfo, error)
	GetProject(string) (*api.Project, string, error)
	GetStoragePoolVolume(pool, volumeType, name string) (*api.StorageVolume, string, error)
	CreateStoragePoolVolume(pool string, request api.StorageVolumesPost) error
}

type sdkGatewayVolumeClient struct { server incus.InstanceServer }

func (c sdkGatewayVolumeClient) GetConnectionInfo() (*incus.ConnectionInfo, error) {
	return c.server.GetConnectionInfo()
}
func (c sdkGatewayVolumeClient) GetProject(name string) (*api.Project, string, error) {
	return c.server.GetProject(name)
}
func (c sdkGatewayVolumeClient) GetStoragePoolVolume(pool, kind, name string) (*api.StorageVolume, string, error) {
	return c.server.GetStoragePoolVolume(pool, kind, name)
}
func (c sdkGatewayVolumeClient) CreateStoragePoolVolume(pool string, request api.StorageVolumesPost) error {
	return c.server.CreateStoragePoolVolume(pool, request)
}

// EnsureLocalGatewayVolume uses a project-bound local Unix connection. It
// leaves the volume untouched on reuse and never performs deletion.
func EnsureLocalGatewayVolume(ctx context.Context, intent GatewayVolumeIntent) (GatewayVolumeResult, error) {
	if err := intent.Validate(); err != nil { return GatewayVolumeResult{}, err }
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil { return GatewayVolumeResult{}, err }
	server, err := incus.ConnectIncusUnixWithContext(ctx, "", &incus.ConnectionArgs{SkipGetServer: true})
	if err != nil { return GatewayVolumeResult{}, observationFailure(ctx, "local Incus volume connection unavailable") }
	defer server.Disconnect()
	return EnsureGatewayVolumeWithClient(ctx, intent, sdkGatewayVolumeClient{server:server.UseProject(intent.Project)})
}

// EnsureGatewayVolumeWithClient only creates an absent owned filesystem
// volume. An existing unmarked, block, or differently marked volume is refused;
// ownership is never inferred from its name. All reads fail closed.
func EnsureGatewayVolumeWithClient(ctx context.Context, intent GatewayVolumeIntent, client GatewayVolumeClient) (GatewayVolumeResult, error) {
	result := GatewayVolumeResult{Project:intent.Project,Pool:intent.Pool,Name:intent.Name}
	fail := func(stage string, attempted bool, cause error) (GatewayVolumeResult,error) {
		return result, &GatewayVolumeError{Stage:stage,Attempted:attempted,Uncertain:attempted,cause:cause}
	}
	if err := intent.Validate(); err != nil { return fail("intent",false,err) }
	if err := ctx.Err(); err != nil { return fail("cancellation",false,err) }
	connection, err := client.GetConnectionInfo()
	if err != nil || connection == nil || connection.Protocol != "incus" ||
		!filepath.IsAbs(connection.SocketPath) || connection.Project != intent.Project {
		return fail("transport",false,fmt.Errorf("local selected-project transport unavailable"))
	}
	scope, _ := incusscope.NewProjectScope(intent.Project)
	checkProject := func() error {
		if err := ctx.Err(); err != nil { return err }
		p, _, err := client.GetProject(scope.Name)
		if err != nil || p == nil || scope.AssessProject(p.Name,p.Config) != incusscope.ProjectCompatible {
			return fmt.Errorf("selected project ownership or policy unverified")
		}
		return ctx.Err()
	}
	inspect := func() (bool,error) {
		if err := ctx.Err(); err != nil { return false,err }
		volume, _, err := client.GetStoragePoolVolume(intent.Pool,"custom",intent.Name)
		if api.StatusErrorCheck(err,http.StatusNotFound) { return false,ctx.Err() }
		if err != nil || volume == nil {
			return false,fmt.Errorf("selected volume observation unavailable")
		}
		if volume.Name != intent.Name || volume.Type != "custom" || volume.ContentType != "filesystem" {
			return false,fmt.Errorf("selected volume type or identity unverified")
		}
		for key,value := range intent.markers() {
			if volume.Config[key] != value { return false,fmt.Errorf("gateway volume ownership unverified") }
		}
		return true,ctx.Err()
	}
	if err := checkProject(); err != nil { return fail("project",false,err) }
	exists,err := inspect()
	if err != nil { return fail("preflight",false,err) }
	if exists {
		result.Verified = true
		return result,nil
	}
	if err := checkProject(); err != nil { return fail("mutation-boundary",false,err) }
	exists,err = inspect()
	if err != nil || exists { return fail("mutation-boundary",false,fmt.Errorf("volume absence not verified at mutation boundary")) }
	if err := ctx.Err(); err != nil { return fail("cancellation",false,err) }
	request := api.StorageVolumesPost{
		Name:intent.Name,Type:"custom",ContentType:"filesystem",
		StorageVolumePut:api.StorageVolumePut{
			Description:"Aginctus managed gateway PostgreSQL data",
			Config:intent.markers(),
		},
	}
	if err := client.CreateStoragePoolVolume(intent.Pool,request); err != nil {
		return fail("create",true,err)
	}
	result.Created = true
	if err := checkProject(); err != nil { return fail("verification",true,err) }
	exists,err = inspect()
	if err != nil || !exists { return fail("verification",true,fmt.Errorf("created volume ownership or identity unverified")) }
	result.Verified = true
	return result,nil
}
