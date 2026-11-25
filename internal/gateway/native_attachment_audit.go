package gateway

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

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
	_, err := observeNativeGatewayAttachments(ctx, top, devices, r, false)
	return err
}

// A stopped-creation preview may explicitly plan a missing volume. Everything
// else, including existing volume ownership and the host prerequisites, still
// requires positive evidence. Only an exact 404 proves absence.
func observeNativeGatewayAttachments(ctx context.Context, top NativeGatewayTopology, devices NativeGatewayDevices, r NativeGatewayAttachmentReader, allowMissingVolume bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	root := devices.Postgres["root"]["pool"]
	data := devices.Postgres["database"]["pool"]
	expected, err := PlanNativeGatewayDevices(top, root, data)
	if err != nil || devices.Project != top.Project ||
		!equalNativeDevices(devices.Postgres, expected.Postgres) ||
		!equalNativeDevices(devices.LiteLLM, expected.LiteLLM) {
		return false, fmt.Errorf("native gateway attachment plan unverified")
	}
	scope, _ := incusscope.NewProjectScope(top.Project)
	info, err := r.GetConnectionInfo()
	if err != nil || info == nil || info.Protocol != "incus" || !filepath.IsAbs(info.SocketPath) || info.Project != scope.Name {
		return false, fmt.Errorf("native gateway attachment transport unverified")
	}
	project, _, err := r.GetProject(scope.Name)
	if err != nil || project == nil || scope.AssessProject(project.Name, project.Config) != incusscope.ProjectCompatible {
		return false, fmt.Errorf("native gateway attachment project policy unverified")
	}
	for _, pool := range []string{root, data} {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		got, _, err := r.GetStoragePool(pool)
		if err != nil || got == nil || got.Name != pool || got.Status != "Created" {
			return false, fmt.Errorf("native gateway selected storage pool unverified")
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
			return false, err
		}
		n, _, err := r.GetNetwork(entry.name)
		if err != nil || n == nil || n.Name != entry.name || !n.Managed || n.Type != "bridge" ||
			n.Config["user.aginctus.managed"] != "true" ||
			n.Config["user.aginctus.resource"] != entry.resource {
			return false, fmt.Errorf("native gateway host bridge ownership unverified")
		}
		if entry.resource == "management-network" &&
			(n.Config["ipv4.nat"] != "false" || n.Config["ipv4.routing"] != "false") {
			return false, fmt.Errorf("native gateway management bridge isolation unverified")
		}
		if entry.resource == "gateway-egress-network" &&
			(n.Config["ipv4.nat"] != "true" || n.Config["ipv4.routing"] != "true") {
			return false, fmt.Errorf("native gateway egress bridge isolation unverified")
		}
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	v, _, err := r.GetStoragePoolVolume(data, "custom", top.DatabaseVolume)
	if allowMissingVolume && api.StatusErrorCheck(err, http.StatusNotFound) {
		return false, ctx.Err()
	}
	if err != nil {
		return false, fmt.Errorf("native gateway PostgreSQL storage observation unavailable")
	}
	postgresName := "postgres-1"
	if top.EnvironmentID != "" {
		postgresName = "ag-" + strings.ReplaceAll(top.EnvironmentID, "-", "") + "-pg"
	}
	if err := verifyGatewayVolume(v, scope.Name, top.EnvironmentID, top.DatabaseVolume, postgresName); err != nil {
		return false, err
	}
	return true, ctx.Err()
}

// Both create/reuse and attachment preflight must agree on the volume owner
// and the only permitted consumer. Legacy topology is audited but not created.
func verifyGatewayVolume(v *api.StorageVolume, project, environmentID, name, postgresName string) error {
	if v == nil || v.Name != name ||
		v.Type != "custom" || v.ContentType != "filesystem" ||
		(environmentID != "" && v.Project != project) {
		return fmt.Errorf("native gateway PostgreSQL storage ownership unverified")
	}
	for key, value := range gatewayVolumeMarkers(project, environmentID) {
		if v.Config[key] != value {
			return fmt.Errorf("native gateway PostgreSQL storage ownership unverified")
		}
	}
	if len(v.UsedBy) > 1 {
		return fmt.Errorf("native gateway PostgreSQL volume attachment is not exclusive")
	}
	for _, ref := range v.UsedBy {
		u, err := url.Parse(ref)
		if err != nil || u.Scheme != "" || u.Host != "" || u.Fragment != "" || u.Path != "/1.0/instances/"+postgresName {
			return fmt.Errorf("native gateway PostgreSQL volume has foreign attachment")
		}
		query, err := url.ParseQuery(u.RawQuery)
		if err != nil || len(query) != 1 || len(query["project"]) != 1 || query.Get("project") != project {
			return fmt.Errorf("native gateway PostgreSQL volume has foreign attachment")
		}
	}
	return nil
}
