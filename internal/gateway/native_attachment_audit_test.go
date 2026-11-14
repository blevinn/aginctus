package gateway

import (
	"context"
	"testing"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

type fakeGatewayAttachmentReader struct {
	info     *incusclient.ConnectionInfo
	project  *api.Project
	networks map[string]*api.Network
	pools    map[string]*api.StoragePool
	volume   *api.StorageVolume
}

func (f *fakeGatewayAttachmentReader) GetConnectionInfo() (*incusclient.ConnectionInfo, error) {
	return f.info, nil
}
func (f *fakeGatewayAttachmentReader) GetProject(string) (*api.Project, string, error) {
	return f.project, "", nil
}
func (f *fakeGatewayAttachmentReader) GetNetwork(s string) (*api.Network, string, error) {
	return f.networks[s], "", nil
}
func (f *fakeGatewayAttachmentReader) GetStoragePool(s string) (*api.StoragePool, string, error) {
	return f.pools[s], "", nil
}
func (f *fakeGatewayAttachmentReader) GetStoragePoolVolume(_, _, _ string) (*api.StorageVolume, string, error) {
	return f.volume, "", nil
}

func attachmentFixture(t *testing.T) (NativeGatewayTopology, NativeGatewayDevices, *fakeGatewayAttachmentReader) {
	t.Helper()
	top := nativePinnedFixture()
	d, err := PlanNativeGatewayDevices(top, "rootpool", "datapool")
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := incusscope.NewProjectScope(top.Project)
	f := &fakeGatewayAttachmentReader{
		info:    &incusclient.ConnectionInfo{Protocol: "incus", SocketPath: "/run/incus/unix.socket", Project: top.Project},
		project: &api.Project{Name: top.Project, ProjectPut: api.ProjectPut{Config: scope.DesiredConfig()}},
		pools: map[string]*api.StoragePool{
			"rootpool": {Name: "rootpool", Status: "Created"},
			"datapool": {Name: "datapool", Status: "Created"},
		},
		networks: map[string]*api.Network{
			"mgmt": {Name: "mgmt", Managed: true, Type: "bridge", NetworkPut: api.NetworkPut{Config: map[string]string{
				"user.aginctus.managed": "true", "user.aginctus.resource": "management-network",
				"ipv4.nat": "false", "ipv4.routing": "false",
			}}},
			"egress": {Name: "egress", Managed: true, Type: "bridge", NetworkPut: api.NetworkPut{Config: map[string]string{
				"user.aginctus.managed": "true", "user.aginctus.resource": "gateway-egress-network",
				"ipv4.nat": "true", "ipv4.routing": "true",
			}}},
		},
		volume: &api.StorageVolume{Name: "gateway-postgres", Type: "custom", ContentType: "filesystem",
			StorageVolumePut: api.StorageVolumePut{Config: map[string]string{
				"user.aginctus.managed": "true", "user.aginctus.resource": "gateway-postgres-volume",
				"user.aginctus.deployment": top.Project, "security.shifted": "true",
			}}},
	}
	return top, d, f
}
func TestNativeGatewayAttachmentAudit(t *testing.T) {
	top, d, f := attachmentFixture(t)
	if err := ObserveNativeGatewayAttachmentsWithReader(context.Background(), top, d, f); err != nil {
		t.Fatal(err)
	}
	f.volume.UsedBy = []string{"/1.0/instances/postgres-1?project=gw"}
	if err := ObserveNativeGatewayAttachmentsWithReader(context.Background(), top, d, f); err != nil {
		t.Fatal(err)
	}
	f.volume.UsedBy = []string{"/1.0/instances/foreign?project=gw"}
	if err := ObserveNativeGatewayAttachmentsWithReader(context.Background(), top, d, f); err == nil {
		t.Fatal("foreign volume attachment accepted")
	}
}
func TestNativeGatewayAttachmentAuditRejectsHostDrift(t *testing.T) {
	for name, mutate := range map[string]func(*fakeGatewayAttachmentReader){
		"foreign bridge":   func(f *fakeGatewayAttachmentReader) { f.networks["mgmt"].Config["user.aginctus.resource"] = "foreign" },
		"egress disabled":  func(f *fakeGatewayAttachmentReader) { f.networks["egress"].Config["ipv4.nat"] = "false" },
		"missing volume":   func(f *fakeGatewayAttachmentReader) { f.volume = nil },
		"storage drift":    func(f *fakeGatewayAttachmentReader) { f.volume.Config["security.shifted"] = "false" },
		"unavailable pool": func(f *fakeGatewayAttachmentReader) { f.pools["rootpool"] = nil },
		"wrong project":    func(f *fakeGatewayAttachmentReader) { f.info.Project = "default" },
	} {
		t.Run(name, func(t *testing.T) {
			top, d, f := attachmentFixture(t)
			mutate(f)
			if err := ObserveNativeGatewayAttachmentsWithReader(context.Background(), top, d, f); err == nil {
				t.Fatal("foreign or drifted resource accepted")
			}
		})
	}
}
