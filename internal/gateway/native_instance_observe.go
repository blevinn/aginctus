package gateway

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// NativeGatewayInstanceStatus is observation only: neither a deployment
// authorization nor permission to adopt or mutate an existing instance.
type NativeGatewayInstanceStatus string

const (
	NativeGatewayAbsent     NativeGatewayInstanceStatus = "absent"
	NativeGatewayCompatible NativeGatewayInstanceStatus = "compatible"
)

type NativeGatewayInstanceObservation struct {
	Name    string                      `json:"name"`
	Project string                      `json:"project"`
	Status  NativeGatewayInstanceStatus `json:"status"`
}

// NativeGatewayInstanceReader is deliberately read-only. Missing instances
// must be confirmed by an exact 404; unknown or inaccessible state fails closed.
type NativeGatewayInstanceReader interface {
	GetConnectionInfo() (*incusclient.ConnectionInfo, error)
	GetProject(string) (*api.Project, string, error)
	GetInstance(string) (*api.Instance, string, error)
	GetImageAlias(string) (*api.ImageAliasesEntry, string, error)
	GetImage(string) (*api.Image, string, error)
}

type nativeGatewaySDKReader struct{ server incusclient.InstanceServer }

func (r nativeGatewaySDKReader) GetConnectionInfo() (*incusclient.ConnectionInfo, error) {
	return r.server.GetConnectionInfo()
}
func (r nativeGatewaySDKReader) GetProject(s string) (*api.Project, string, error) {
	return r.server.GetProject(s)
}
func (r nativeGatewaySDKReader) GetInstance(s string) (*api.Instance, string, error) {
	return r.server.GetInstance(s)
}
func (r nativeGatewaySDKReader) GetImageAlias(s string) (*api.ImageAliasesEntry, string, error) {
	return r.server.GetImageAlias(s)
}
func (r nativeGatewaySDKReader) GetImage(s string) (*api.Image, string, error) {
	return r.server.GetImage(s)
}

// ObserveLocalNativeGatewayInstances opens only the local Unix socket, bound
// to the explicitly selected nondefault project. It does not modify the daemon.
func ObserveLocalNativeGatewayInstances(ctx context.Context, pair NativeGatewayInstancePair) ([]NativeGatewayInstanceObservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if pair.Postgres.Project != pair.LiteLLM.Project {
		return nil, fmt.Errorf("native gateway selected projects differ")
	}
	if _, err := incusscope.NewProjectScope(pair.Postgres.Project); err != nil {
		return nil, err
	}
	server, err := incusclient.ConnectIncusUnixWithContext(ctx, "", &incusclient.ConnectionArgs{SkipGetServer: true})
	if err != nil {
		return nil, fmt.Errorf("native gateway local observation unavailable")
	}
	defer server.Disconnect()
	return ObserveNativeGatewayInstancesWithReader(ctx, pair, nativeGatewaySDKReader{server: server.UseProject(pair.Postgres.Project)})
}

// ObserveNativeGatewayInstancesWithReader rejects legacy compose instances
// and anything with unknown ownership, devices, image or scope. The exact
// service marker is intentionally distinct from workload/compose markers.
func ObserveNativeGatewayInstancesWithReader(ctx context.Context, pair NativeGatewayInstancePair, reader NativeGatewayInstanceReader) ([]NativeGatewayInstanceObservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if pair.Postgres.Project != pair.LiteLLM.Project || pair.Postgres.Name != "postgres-1" ||
		pair.LiteLLM.Name != "litellm-1" || pair.Postgres.Role != "postgres" ||
		pair.LiteLLM.Role != "litellm" || pair.Postgres.ImageAlias == pair.LiteLLM.ImageAlias {
		return nil, fmt.Errorf("native gateway instance identities invalid")
	}
	scope, err := incusscope.NewProjectScope(pair.Postgres.Project)
	if err != nil {
		return nil, err
	}
	info, err := reader.GetConnectionInfo()
	if err != nil || info == nil || info.Protocol != "incus" || !filepath.IsAbs(info.SocketPath) ||
		info.Project != scope.Name {
		return nil, fmt.Errorf("native gateway selected-project transport unverified")
	}
	project, _, err := reader.GetProject(scope.Name)
	if err != nil || project == nil || scope.AssessProject(project.Name, project.Config) != incusscope.ProjectCompatible {
		return nil, fmt.Errorf("native gateway project policy unverified")
	}
	result := make([]NativeGatewayInstanceObservation, 0, 2)
	for _, spec := range []NativeGatewayInstanceSpec{pair.Postgres, pair.LiteLLM} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if spec.Project != scope.Name || !nativeDeviceName.MatchString(spec.ImageAlias) || len(spec.Devices) == 0 {
			return nil, fmt.Errorf("native gateway instance intent incomplete")
		}
		alias, _, err := reader.GetImageAlias(spec.ImageAlias)
		if err != nil || alias == nil || alias.Name != spec.ImageAlias {
			return nil, fmt.Errorf("native gateway image alias unverified")
		}
		image, _, err := reader.GetImage(alias.Target)
		if err != nil || image == nil || image.Project != scope.Name || image.Type != "container" ||
			image.Fingerprint != alias.Target {
			return nil, fmt.Errorf("native gateway image identity unverified")
		}
		inst, _, err := reader.GetInstance(spec.Name)
		if api.StatusErrorCheck(err, http.StatusNotFound) {
			result = append(result, NativeGatewayInstanceObservation{Name: spec.Name, Project: scope.Name, Status: NativeGatewayAbsent})
			continue
		}
		if err != nil || inst == nil || inst.Name != spec.Name || inst.Project != scope.Name ||
			string(inst.Type) != "container" || inst.Config["user.aginctus.managed"] != "true" ||
			inst.Config["user.aginctus.resource"] != "gateway-instance" ||
			inst.Config["user.aginctus.role"] != spec.Role ||
			inst.Config["user.aginctus.deployment"] != scope.Name ||
			inst.Config["volatile.base_image"] != alias.Target ||
			!reflect.DeepEqual(inst.Devices, spec.Devices) || len(inst.Profiles) != 0 {
			return nil, fmt.Errorf("native gateway existing instance ownership or attachments unverified")
		}
		result = append(result, NativeGatewayInstanceObservation{Name: spec.Name, Project: scope.Name, Status: NativeGatewayCompatible})
	}
	return result, ctx.Err()
}
