package environment

import (
	"context"
	"maps"
	"strings"
	"testing"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

type projectServiceInspectionFake struct {
	connection    incusclient.ConnectionInfo
	server        api.Server
	project       api.Project
	projectETag   string
	instance      api.Instance
	volume        api.StorageVolume
	instanceETag  string
	volumeETag    string
	reads         int
	secondDevices api.DevicesMap
}

func (f *projectServiceInspectionFake) GetConnectionInfo() (*incusclient.ConnectionInfo, error) {
	return &f.connection, nil
}
func (f *projectServiceInspectionFake) GetProject(string) (*api.Project, string, error) {
	return &f.project, f.projectETag, nil
}
func (f *projectServiceInspectionFake) GetServer() (*api.Server, string, error) {
	return &f.server, "server-etag", nil
}
func (f *projectServiceInspectionFake) GetInstance(string) (*api.Instance, string, error) {
	f.reads++
	if f.reads == 2 && f.secondDevices != nil {
		i := f.instance
		i.ExpandedDevices = f.secondDevices
		return &i, f.instanceETag, nil
	}
	return &f.instance, f.instanceETag, nil
}
func (f *projectServiceInspectionFake) GetStoragePoolVolume(string, string, string) (*api.StorageVolume, string, error) {
	return &f.volume, f.volumeETag, nil
}
func projectAttachmentFixture() (ProjectServiceAttachment, *projectServiceInspectionFake) {
	want := ProjectServiceAttachment{
		Binding:  Binding{ServerID: "sha256:" + strings.Repeat("a", 64), Project: "dev-team"},
		Instance: "state-service", Pool: "statepool", Volume: "accepted-state",
		Device: "state", MountPath: "/var/lib/aginctus",
	}
	f := &projectServiceInspectionFake{
		connection: incusclient.ConnectionInfo{Protocol: "incus", SocketPath: "/var/lib/incus/unix.socket", Project: want.Binding.Project},
		server:     api.Server{ServerUntrusted: api.ServerUntrusted{Auth: "trusted"}, Environment: api.ServerEnvironment{Project: want.Binding.Project, CertificateFingerprint: strings.Repeat("a", 64)}},
		project: api.Project{Name: want.Binding.Project, ProjectPut: api.ProjectPut{
			Config: incusscope.ProjectScope{Name: want.Binding.Project}.DesiredConfig(),
		}},
		projectETag: "project-etag",
		instance: api.Instance{
			Name: want.Instance, Project: want.Binding.Project, Type: "container",
			InstancePut: api.InstancePut{
				Config: api.ConfigMap{
					"user.aginctus.managed":  "true",
					"user.aginctus.resource": "project-state-service",
				},
				Devices: api.DevicesMap{
					"state": {"type": "disk", "pool": want.Pool, "source": want.Volume, "path": want.MountPath},
				},
			},
		},
		volume: api.StorageVolume{
			Name: want.Volume, Type: "custom", ContentType: "filesystem", Project: want.Binding.Project,
			StorageVolumePut: api.StorageVolumePut{Config: map[string]string{
				"user.aginctus.managed":  "true",
				"user.aginctus.resource": "project-state-volume",
			}},
			UsedBy: []string{"/1.0/instances/state-service?project=dev-team"},
		},
		instanceETag: "instance-etag", volumeETag: "volume-etag",
	}
	f.project.Config["restricted"] = "true"
	f.instance.ExpandedConfig = maps.Clone(f.instance.Config)
	f.instance.ExpandedDevices = maps.Clone(f.instance.Devices)
	f.instance.ExpandedDevices["root"] = map[string]string{"type": "disk", "path": "/", "pool": "rootpool"}
	return want, f
}
func TestProjectServiceAttachmentVerifiesExclusiveObservedVolume(t *testing.T) {
	want, f := projectAttachmentFixture()
	if err := VerifyProjectServiceAttachment(context.Background(), f, want); err != nil {
		t.Fatal(err)
	}
	if f.reads != 2 {
		t.Fatal("service instance was not reobserved")
	}
}
func TestProjectServiceAttachmentFailsClosedOnForeignAndAmbiguousResources(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*ProjectServiceAttachment, *projectServiceInspectionFake)
	}{
		{"cross-project connection", func(_ *ProjectServiceAttachment, f *projectServiceInspectionFake) { f.connection.Project = "other" }},
		{"missing socket", func(_ *ProjectServiceAttachment, f *projectServiceInspectionFake) { f.connection.SocketPath = "" }},
		{"wrong server pin", func(_ *ProjectServiceAttachment, f *projectServiceInspectionFake) {
			f.server.Environment.CertificateFingerprint = strings.Repeat("b", 64)
		}},
		{"other socket", func(_ *ProjectServiceAttachment, f *projectServiceInspectionFake) {
			f.connection.SocketPath = "/run/other.socket"
		}},
		{"unrestricted project", func(_ *ProjectServiceAttachment, f *projectServiceInspectionFake) {
			f.project.Config["restricted"] = "false"
		}},
		{"project storage isolation off", func(_ *ProjectServiceAttachment, f *projectServiceInspectionFake) {
			f.project.Config["features.storage.volumes"] = "false"
		}},
		{"project ownership forged", func(_ *ProjectServiceAttachment, f *projectServiceInspectionFake) {
			f.project.Config["user.aginctus.deployment"] = "foreign"
		}},
		{"foreign instance", func(_ *ProjectServiceAttachment, f *projectServiceInspectionFake) { f.instance.Project = "other" }},
		{"unmarked instance", func(_ *ProjectServiceAttachment, f *projectServiceInspectionFake) {
			f.instance.Config["user.aginctus.resource"] = "foreign"
		}},
		{"foreign device", func(_ *ProjectServiceAttachment, f *projectServiceInspectionFake) {
			f.instance.Devices["state"]["pool"] = "other"
		}},
		{"extra device property", func(_ *ProjectServiceAttachment, f *projectServiceInspectionFake) {
			f.instance.Devices["state"]["readonly"] = "true"
		}},
		{"extra volume", func(_ *ProjectServiceAttachment, f *projectServiceInspectionFake) {
			f.instance.Devices["other"] = map[string]string{"type": "disk", "pool": "x", "source": "y", "path": "/other"}
		}},
		{"inherited host disk", func(_ *ProjectServiceAttachment, f *projectServiceInspectionFake) {
			f.instance.ExpandedDevices["other"] = map[string]string{"type": "disk", "source": "/host/data", "path": "/other"}
		}},
		{"inherited privileged configuration", func(_ *ProjectServiceAttachment, f *projectServiceInspectionFake) {
			f.instance.ExpandedConfig["security.privileged"] = "true"
		}},
		{"inherited socket", func(_ *ProjectServiceAttachment, f *projectServiceInspectionFake) {
			f.instance.ExpandedDevices["other"] = map[string]string{"type": "proxy"}
		}},
		{"unknown effective devices", func(_ *ProjectServiceAttachment, f *projectServiceInspectionFake) { f.instance.ExpandedDevices = nil }},
		{"effective device changed at stable ETag", func(w *ProjectServiceAttachment, f *projectServiceInspectionFake) {
			w.Network = "state-mgmt"
			f.instance.ExpandedDevices["eth0"] = map[string]string{"type": "nic", "name": "eth0", "network": w.Network}
			f.secondDevices = maps.Clone(f.instance.ExpandedDevices)
			f.secondDevices["extra"] = map[string]string{"type": "disk", "path": "/extra", "source": "/host/data"}
		}},
		{"foreign volume project", func(_ *ProjectServiceAttachment, f *projectServiceInspectionFake) { f.volume.Project = "foreign" }},
		{"foreign volume marker", func(_ *ProjectServiceAttachment, f *projectServiceInspectionFake) {
			f.volume.Config["user.aginctus.resource"] = "foreign"
		}},
		{"shared volume", func(_ *ProjectServiceAttachment, f *projectServiceInspectionFake) {
			f.volume.UsedBy = append(f.volume.UsedBy, "/1.0/instances/foreign?project=dev-team")
		}},
		{"wrong project attachment", func(_ *ProjectServiceAttachment, f *projectServiceInspectionFake) {
			f.volume.UsedBy[0] = "/1.0/instances/state-service?project=foreign"
		}},
		{"unsupported implicit project", func(_ *ProjectServiceAttachment, f *projectServiceInspectionFake) {
			f.volume.UsedBy[0] = "/1.0/instances/state-service"
		}},
		{"no etag", func(_ *ProjectServiceAttachment, f *projectServiceInspectionFake) { f.volumeETag = "" }},
		{"invalid requested root mount", func(w *ProjectServiceAttachment, _ *projectServiceInspectionFake) { w.MountPath = "/" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want, f := projectAttachmentFixture()
			tc.change(&want, f)
			if err := VerifyProjectServiceAttachment(context.Background(), f, want); err == nil {
				t.Fatal("accepted unverified project state volume attachment")
			}
		})
	}
}
