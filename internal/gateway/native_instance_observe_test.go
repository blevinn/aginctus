package gateway

import (
	"context"
	"net/http"
	"strings"
	"testing"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

type fakeGatewayInstanceReader struct {
	info      *incusclient.ConnectionInfo
	project   *api.Project
	instances map[string]*api.Instance
	aliases   map[string]*api.ImageAliasesEntry
	images    map[string]*api.Image
}

func (f fakeGatewayInstanceReader) GetConnectionInfo() (*incusclient.ConnectionInfo, error) {
	return f.info, nil
}
func (f fakeGatewayInstanceReader) GetProject(string) (*api.Project, string, error) {
	return f.project, "", nil
}
func (f fakeGatewayInstanceReader) GetInstance(name string) (*api.Instance, string, error) {
	if v := f.instances[name]; v != nil {
		return v, "etag", nil
	}
	return nil, "", api.StatusErrorf(http.StatusNotFound, "not found")
}
func (f fakeGatewayInstanceReader) GetImageAlias(name string) (*api.ImageAliasesEntry, string, error) {
	if v := f.aliases[name]; v != nil {
		return v, "", nil
	}
	return nil, "", api.StatusErrorf(http.StatusNotFound, "not found")
}
func (f fakeGatewayInstanceReader) GetImage(name string) (*api.Image, string, error) {
	if v := f.images[name]; v != nil {
		return v, "", nil
	}
	return nil, "", api.StatusErrorf(http.StatusNotFound, "not found")
}

func instanceReaderFixture(t *testing.T) (NativeGatewayInstancePair, *fakeGatewayInstanceReader) {
	t.Helper()
	top := nativePinnedFixture()
	d, err := PlanNativeGatewayDevices(top, "root", "data")
	if err != nil {
		t.Fatal(err)
	}
	pair, err := PlanNativeGatewayInstances(top, d, "pg-image", "llm-image")
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := incusscope.NewProjectScope(top.Project)
	fp1 := strings.Repeat("a", 64)
	fp2 := strings.Repeat("b", 64)
	f := &fakeGatewayInstanceReader{
		info:      &incusclient.ConnectionInfo{Protocol: "incus", SocketPath: "/run/incus/unix.socket", Project: top.Project},
		project:   &api.Project{Name: top.Project, ProjectPut: api.ProjectPut{Config: scope.DesiredConfig()}},
		instances: map[string]*api.Instance{},
		aliases: map[string]*api.ImageAliasesEntry{
			"pg-image":  {Name: "pg-image", ImageAliasesEntryPut: api.ImageAliasesEntryPut{Target: fp1}},
			"llm-image": {Name: "llm-image", ImageAliasesEntryPut: api.ImageAliasesEntryPut{Target: fp2}},
		},
		images: map[string]*api.Image{
			fp1: {Project: top.Project, Type: "container", Fingerprint: fp1},
			fp2: {Project: top.Project, Type: "container", Fingerprint: fp2},
		},
	}
	return pair, f
}
func TestObserveNativeGatewayInstancesAbsentAndOwned(t *testing.T) {
	pair, f := instanceReaderFixture(t)
	out, err := ObserveNativeGatewayInstancesWithReader(context.Background(), pair, f)
	if err != nil || len(out) != 2 || out[0].Status != NativeGatewayAbsent || out[1].Status != NativeGatewayAbsent {
		t.Fatalf("absent: %+v %v", out, err)
	}
	for _, spec := range []NativeGatewayInstanceSpec{pair.Postgres, pair.LiteLLM} {
		fp := f.aliases[spec.ImageAlias].Target
		f.instances[spec.Name] = &api.Instance{
			Name: spec.Name, Project: spec.Project, Type: api.InstanceTypeContainer,
			InstancePut: api.InstancePut{Profiles: []string{}, Devices: spec.Devices, Config: map[string]string{
				"user.aginctus.managed": "true", "user.aginctus.resource": "gateway-instance",
				"user.aginctus.role": spec.Role, "user.aginctus.deployment": spec.Project,
				"volatile.base_image": fp,
			}},
		}
	}
	out, err = ObserveNativeGatewayInstancesWithReader(context.Background(), pair, f)
	if err != nil || len(out) != 2 || out[0].Status != NativeGatewayCompatible || out[1].Status != NativeGatewayCompatible {
		t.Fatalf("owned: %+v %v", out, err)
	}
	f.instances["litellm-1"].Devices["host"] = map[string]string{"type": "disk", "source": "/etc", "path": "/host"}
	if _, err := ObserveNativeGatewayInstancesWithReader(context.Background(), pair, f); err == nil {
		t.Fatal("unexpected host device accepted")
	}
}
func TestObserveNativeGatewayInstancesRejectsLegacyAndForeign(t *testing.T) {
	pair, f := instanceReaderFixture(t)
	f.info.Project = "default"
	if _, err := ObserveNativeGatewayInstancesWithReader(context.Background(), pair, f); err == nil {
		t.Fatal("wrong project accepted")
	}
	f.info.Project = pair.Postgres.Project
	f.instances["postgres-1"] = &api.Instance{
		Name: "postgres-1", Project: pair.Postgres.Project, Type: api.InstanceTypeContainer,
		InstancePut: api.InstancePut{Config: map[string]string{"user.aginctus.managed": "true", "user.aginctus.resource": "compose-instance"}},
	}
	if _, err := ObserveNativeGatewayInstancesWithReader(context.Background(), pair, f); err == nil {
		t.Fatal("legacy instance adopted")
	}
}
