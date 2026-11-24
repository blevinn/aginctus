package gateway

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"reflect"
	"strings"
	"testing"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
)

const volumeOwnerKey = "user.aginctus.environment-id"

type fakeGatewayVolumeClient struct {
	*fakeGatewayAttachmentReader
	server   api.Server
	volumes  map[string]*api.StorageVolume
	reads    []string
	creates  int
	getErr   error
	onCreate func(*api.StorageVolume) error
}

func (f *fakeGatewayVolumeClient) GetServer() (*api.Server, string, error) {
	return &f.server, "", nil
}

func (f *fakeGatewayVolumeClient) GetStoragePoolVolume(pool, kind, name string) (*api.StorageVolume, string, error) {
	f.reads = append(f.reads, pool+"/"+kind+"/"+name)
	if f.getErr != nil {
		return nil, "", f.getErr
	}
	if volume := f.volumes[name]; volume != nil {
		return volume, "", nil
	}
	return nil, "", api.StatusErrorf(http.StatusNotFound, "absent")
}

func (f *fakeGatewayVolumeClient) CreateStoragePoolVolume(pool string, req api.StorageVolumesPost) error {
	f.creates++
	if pool != "datapool" || f.volumes[req.Name] != nil {
		return errors.New("unexpected create target")
	}
	v := &api.StorageVolume{Name: req.Name, Type: req.Type, ContentType: req.ContentType,
		Project: f.info.Project, StorageVolumePut: req.StorageVolumePut}
	f.volumes[req.Name] = v
	if f.onCreate != nil {
		return f.onCreate(v)
	}
	return nil
}

func gatewayVolumeFixture(t *testing.T) (environment.Descriptor, NativeGatewayResourceIdentity, *fakeGatewayVolumeClient) {
	t.Helper()
	top, _, reader := nativeAttachmentAuditFixture(t)
	d := gatewayTestEnvironment("123e4567-e89b-42d3-a456-426614174000", top.Project)
	fingerprint := strings.Repeat("a", 64)
	d.Binding.ServerID = "sha256:" + fingerprint
	id, err := NativeGatewayIdentity(d, d.Binding)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeGatewayVolumeClient{fakeGatewayAttachmentReader: reader, volumes: map[string]*api.StorageVolume{}}
	f.server.Environment.Project = top.Project
	f.server.Environment.CertificateFingerprint = fingerprint
	return d, id, f
}

func seedGatewayVolume(id NativeGatewayResourceIdentity) *api.StorageVolume {
	return &api.StorageVolume{Name: id.DatabaseVolume, Type: "custom", ContentType: "filesystem", Project: id.Project,
		StorageVolumePut: api.StorageVolumePut{Config: gatewayVolumeMarkers(id.Project, id.EnvironmentID)}}
}

func TestGatewayVolumeCreateAndRepeatLeaveSiblingUntouched(t *testing.T) {
	d, id, f := gatewayVolumeFixture(t)
	sibling := d
	sibling.ID = "123e4567-e89b-42d3-a456-426614174001"
	siblingID, err := NativeGatewayIdentity(sibling, sibling.Binding)
	if err != nil {
		t.Fatal(err)
	}
	siblingVolume := seedGatewayVolume(siblingID)
	f.volumes[siblingID.DatabaseVolume] = siblingVolume
	before := *siblingVolume
	before.Config = maps.Clone(siblingVolume.Config)
	guards := 0
	for attempt := range 2 {
		r, err := EnsureNativeEnvironmentGatewayVolumeWithClient(context.Background(), d, d.Binding, "datapool", f,
			func(context.Context) error { guards++; return nil })
		if err != nil || !r.Verified || r.Created != (attempt == 0) || r.Name != id.DatabaseVolume ||
			r.EnvironmentID != d.ID || guards != 1 || f.creates != 1 {
			t.Fatalf("attempt %d: %+v %v (guards=%d creates=%d)", attempt, r, err, guards, f.creates)
		}
	}
	if got := f.volumes[id.DatabaseVolume].Config[volumeOwnerKey]; got != d.ID {
		t.Fatalf("created volume owner = %q", got)
	}
	if len(f.volumes) != 2 || !reflect.DeepEqual(f.volumes[siblingID.DatabaseVolume], &before) {
		t.Fatal("sibling or inventory changed")
	}
	for _, read := range f.reads {
		if read != "datapool/custom/"+id.DatabaseVolume {
			t.Fatalf("unselected read: %s", read)
		}
	}
}

func TestGatewayVolumeRefusesIncompatibleObservation(t *testing.T) {
	for name, mutate := range map[string]func(*api.StorageVolume){
		"missing owner": func(v *api.StorageVolume) { delete(v.Config, volumeOwnerKey) },
		"foreign owner": func(v *api.StorageVolume) { v.Config[volumeOwnerKey] = "123e4567-e89b-42d3-a456-426614174001" },
		"block":         func(v *api.StorageVolume) { v.ContentType = "block" },
		"unshifted":     func(v *api.StorageVolume) { delete(v.Config, "security.shifted") },
		"wrong project": func(v *api.StorageVolume) { v.Project = "default" },
		"wrong name":    func(v *api.StorageVolume) { v.Name = "gateway-postgres" },
		"foreign attachment": func(v *api.StorageVolume) {
			v.UsedBy = []string{"/1.0/instances/postgres-1?project=" + v.Project}
		},
	} {
		t.Run(name, func(t *testing.T) {
			d, id, f := gatewayVolumeFixture(t)
			v := seedGatewayVolume(id)
			mutate(v)
			f.volumes[id.DatabaseVolume] = v
			before := *v
			before.Config = maps.Clone(v.Config)
			r, err := EnsureNativeEnvironmentGatewayVolumeWithClient(context.Background(), d, d.Binding, "datapool", f,
				func(context.Context) error { t.Fatal("authorized despite incompatible observation"); return nil })
			if err == nil || r.Verified || r.Created || f.creates != 0 || !reflect.DeepEqual(f.volumes[id.DatabaseVolume], &before) {
				t.Fatalf("collision accepted or modified: %+v %v", r, err)
			}
		})
	}
}

func TestGatewayVolumeRequiresBindingAndWriteAuthority(t *testing.T) {
	for _, name := range []string{"missing guard", "denied guard", "selected binding", "wrong transport", "daemon fingerprint", "daemon project", "project policy", "unknown volume", "boundary drift", "boundary collision", "canceled"} {
		t.Run(name, func(t *testing.T) {
			d, id, f := gatewayVolumeFixture(t)
			selected := d.Binding
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			guards := 0
			guard := func(context.Context) error {
				guards++
				switch name {
				case "boundary drift":
					f.server.Environment.CertificateFingerprint = strings.Repeat("b", 64)
				case "boundary collision":
					f.volumes[id.DatabaseVolume] = seedGatewayVolume(id)
				case "denied guard":
					return errors.New("denied")
				case "canceled":
					cancel()
				}
				return nil
			}
			wantGuards := 0
			switch name {
			case "missing guard":
				guard = nil
			case "selected binding":
				selected.ServerID = "sha256:" + strings.Repeat("b", 64)
			case "wrong transport":
				f.info = &incus.ConnectionInfo{Protocol: "https", Project: id.Project}
			case "daemon fingerprint":
				f.server.Environment.CertificateFingerprint = ""
			case "daemon project":
				f.server.Environment.Project = "default"
			case "project policy":
				f.project.Config["features.storage.volumes"] = "false"
			case "unknown volume":
				f.getErr = api.StatusErrorf(http.StatusForbidden, "denied")
			default:
				wantGuards = 1
			}
			r, err := EnsureNativeEnvironmentGatewayVolumeWithClient(ctx, d, selected, "datapool", f, guard)
			if err == nil || r.Created || r.Verified || f.creates != 0 || guards != wantGuards {
				t.Fatalf("unsafe write: %+v %v (guards=%d creates=%d)", r, err, guards, f.creates)
			}
			if name == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
		})
	}
}

func TestGatewayVolumeUncertainAfterAttempt(t *testing.T) {
	for _, name := range []string{"owner drift", "binding drift", "response lost"} {
		t.Run(name, func(t *testing.T) {
			d, id, f := gatewayVolumeFixture(t)
			f.onCreate = func(v *api.StorageVolume) error {
				switch name {
				case "owner drift":
					delete(v.Config, volumeOwnerKey)
				case "binding drift":
					f.server.Environment.Project = "default"
				case "response lost":
					return errors.New("response lost")
				}
				return nil
			}
			r, err := EnsureNativeEnvironmentGatewayVolumeWithClient(context.Background(), d, d.Binding, "datapool", f,
				func(context.Context) error { return nil })
			var failure *GatewayVolumeError
			if !errors.As(err, &failure) || !failure.Attempted || !failure.Uncertain ||
				r.Created != (name != "response lost") || r.Verified || f.creates != 1 || f.volumes[id.DatabaseVolume] == nil {
				t.Fatalf("uncertain completion: %+v %v", r, err)
			}
		})
	}
}
