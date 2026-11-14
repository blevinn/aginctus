package gateway

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// NativeGatewayAttachmentReader is read-only. The management and egress
// bridges are host resources; inspecting them never authorizes repair.
type NativeGatewayAttachmentReader interface {
	GetConnectionInfo() (*incusclient.ConnectionInfo, error)
	GetProject(string) (*api.Project, string, error)
	GetNetwork(string) (*api.Network, string, error)
	GetStoragePool(string) (*api.StoragePool, string, error)
	GetStoragePoolVolume(string, string, string) (*api.StorageVolume, string, error)
}

type nativeGatewayAttachmentSDK struct{ server incusclient.InstanceServer }

func (r nativeGatewayAttachmentSDK) GetConnectionInfo() (*incusclient.ConnectionInfo, error) {
	return r.server.GetConnectionInfo()
}
func (r nativeGatewayAttachmentSDK) GetProject(s string) (*api.Project, string, error) {
	return r.server.GetProject(s)
}
func (r nativeGatewayAttachmentSDK) GetNetwork(s string) (*api.Network, string, error) {
	return r.server.GetNetwork(s)
}
func (r nativeGatewayAttachmentSDK) GetStoragePool(s string) (*api.StoragePool, string, error) {
	return r.server.GetStoragePool(s)
}
func (r nativeGatewayAttachmentSDK) GetStoragePoolVolume(pool, kind, name string) (*api.StorageVolume, string, error) {
	return r.server.GetStoragePoolVolume(pool, kind, name)
}

// ObserveLocalNativeGatewayAttachments only reads an explicit project via
// the local Incus socket. It does not create or attach any device.
func ObserveLocalNativeGatewayAttachments(ctx context.Context, top NativeGatewayTopology, devices NativeGatewayDevices) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := incusscope.NewProjectScope(top.Project); err != nil {
		return err
	}
	server, err := incusclient.ConnectIncusUnixWithContext(ctx, "", &incusclient.ConnectionArgs{SkipGetServer: true})
	if err != nil {
		return fmt.Errorf("native gateway local attachment observation unavailable")
	}
	defer server.Disconnect()
	return ObserveNativeGatewayAttachmentsWithReader(ctx, top, devices, nativeGatewayAttachmentSDK{server: server.UseProject(top.Project)})
}

// ObserveNativeGatewayAttachmentsWithReader verifies host-owned network
// identities and project-owned durable storage. An unmarked similarly named
// volume or bridge is foreign, never an adoption opportunity. This is not
// permission to modify host resources or a proof of caller authority.
func ObserveNativeGatewayAttachmentsWithReader(ctx context.Context, top NativeGatewayTopology, devices NativeGatewayDevices, r NativeGatewayAttachmentReader) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root := devices.Postgres["root"]["pool"]
	data := devices.Postgres["database"]["pool"]
	expected, err := PlanNativeGatewayDevices(top, root, data)
	if err != nil || devices.Project != top.Project ||
		!equalNativeDevices(devices.Postgres, expected.Postgres) ||
		!equalNativeDevices(devices.LiteLLM, expected.LiteLLM) {
		return fmt.Errorf("native gateway attachment plan unverified")
	}
	scope, _ := incusscope.NewProjectScope(top.Project)
	info, err := r.GetConnectionInfo()
	if err != nil || info == nil || info.Protocol != "incus" || !filepath.IsAbs(info.SocketPath) || info.Project != scope.Name {
		return fmt.Errorf("native gateway attachment transport unverified")
	}
	project, _, err := r.GetProject(scope.Name)
	if err != nil || project == nil || scope.AssessProject(project.Name, project.Config) != incusscope.ProjectCompatible {
		return fmt.Errorf("native gateway attachment project policy unverified")
	}
	for _, pool := range []string{root, data} {
		if err := ctx.Err(); err != nil {
			return err
		}
		got, _, err := r.GetStoragePool(pool)
		if err != nil || got == nil || got.Name != pool || got.Status != "Created" {
			return fmt.Errorf("native gateway selected storage pool unverified")
		}
	}
	for _, entry := range []struct{ name, resource string }{
		{top.ManagementNetwork, "management-network"},
		{top.EgressNetwork, "gateway-egress-network"},
	} {
		if entry.name == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		n, _, err := r.GetNetwork(entry.name)
		if err != nil || n == nil || n.Name != entry.name || !n.Managed || n.Type != "bridge" ||
			n.Config["user.aginctus.managed"] != "true" ||
			n.Config["user.aginctus.resource"] != entry.resource {
			return fmt.Errorf("native gateway host bridge ownership unverified")
		}
		if entry.resource == "management-network" &&
			(n.Config["ipv4.nat"] != "false" || n.Config["ipv4.routing"] != "false") {
			return fmt.Errorf("native gateway management bridge isolation unverified")
		}
		if entry.resource == "gateway-egress-network" &&
			(n.Config["ipv4.nat"] != "true" || n.Config["ipv4.routing"] != "true") {
			return fmt.Errorf("native gateway egress bridge isolation unverified")
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	v, _, err := r.GetStoragePoolVolume(data, "custom", top.DatabaseVolume)
	if err != nil || v == nil || v.Name != top.DatabaseVolume ||
		v.Type != "custom" || v.ContentType != "filesystem" ||
		v.Config["user.aginctus.managed"] != "true" ||
		v.Config["user.aginctus.resource"] != "gateway-postgres-volume" ||
		v.Config["user.aginctus.deployment"] != scope.Name ||
		v.Config["security.shifted"] != "true" {
		return fmt.Errorf("native gateway PostgreSQL storage ownership unverified")
	}
	for _, ref := range v.UsedBy {
		u, err := url.Parse(ref)
		if err != nil || u.Path != "/1.0/instances/postgres-1" ||
			u.Query().Get("project") != scope.Name || len(u.Query()) != 1 {
			return fmt.Errorf("native gateway PostgreSQL volume has foreign attachment")
		}
	}
	return ctx.Err()
}
