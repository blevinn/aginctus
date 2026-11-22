package environment

import (
	"context"
	"testing"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

type projectServiceInspectionFake struct {
	connection incusclient.ConnectionInfo
	instance api.Instance
	volume api.StorageVolume
	instanceETag string
	volumeETag string
	reads int
}
func (f *projectServiceInspectionFake) GetConnectionInfo() (*incusclient.ConnectionInfo,error) {
	return &f.connection,nil
}
func (f *projectServiceInspectionFake) GetInstance(string) (*api.Instance,string,error) {
	f.reads++
	return &f.instance,f.instanceETag,nil
}
func (f *projectServiceInspectionFake) GetStoragePoolVolume(string,string,string) (*api.StorageVolume,string,error) {
	return &f.volume,f.volumeETag,nil
}
func projectAttachmentFixture() (ProjectServiceAttachment,*projectServiceInspectionFake) {
	want:=ProjectServiceAttachment{
		Binding:Binding{ServerID:"verified-server",Project:"dev-team"},
		Instance:"state-service",Pool:"statepool",Volume:"accepted-state",
		Device:"state",MountPath:"/var/lib/aginctus",
	}
	f:=&projectServiceInspectionFake{
		connection:incusclient.ConnectionInfo{Protocol:"incus",SocketPath:"/var/lib/incus/unix.socket",Project:want.Binding.Project},
		instance:api.Instance{
			Name:want.Instance,Project:want.Binding.Project,
			InstancePut:api.InstancePut{
				Config:api.ConfigMap{
					"user.aginctus.managed":"true",
					"user.aginctus.resource":"project-state-service",
				},
				Devices:api.DevicesMap{
					"state":{"type":"disk","pool":want.Pool,"source":want.Volume,"path":want.MountPath},
				},
			},
		},
		volume:api.StorageVolume{
			Name:want.Volume,Type:"custom",ContentType:"filesystem",
			StorageVolumePut:api.StorageVolumePut{Config:map[string]string{
				"user.aginctus.managed":"true",
				"user.aginctus.resource":"project-state-volume",
			}},
			UsedBy:[]string{"/1.0/instances/state-service?project=dev-team"},
		},
		instanceETag:"instance-etag",volumeETag:"volume-etag",
	}
	return want,f
}
func TestProjectServiceAttachmentVerifiesExclusiveObservedVolume(t *testing.T) {
	want,f:=projectAttachmentFixture()
	if err:=VerifyProjectServiceAttachment(context.Background(),f,want);err!=nil {
		t.Fatal(err)
	}
	if f.reads!=2 {t.Fatal("service instance was not reobserved")}
}
func TestProjectServiceAttachmentFailsClosedOnForeignAndAmbiguousResources(t *testing.T) {
	for _,tc:=range []struct{
		name string
		change func(*ProjectServiceAttachment,*projectServiceInspectionFake)
	}{
		{"cross-project connection",func(_ *ProjectServiceAttachment,f *projectServiceInspectionFake){f.connection.Project="other"}},
		{"missing socket",func(_ *ProjectServiceAttachment,f *projectServiceInspectionFake){f.connection.SocketPath=""}},
		{"foreign instance",func(_ *ProjectServiceAttachment,f *projectServiceInspectionFake){f.instance.Project="other"}},
		{"unmarked instance",func(_ *ProjectServiceAttachment,f *projectServiceInspectionFake){f.instance.Config["user.aginctus.resource"]="foreign"}},
		{"foreign device",func(_ *ProjectServiceAttachment,f *projectServiceInspectionFake){f.instance.Devices["state"]["pool"]="other"}},
		{"extra device property",func(_ *ProjectServiceAttachment,f *projectServiceInspectionFake){f.instance.Devices["state"]["readonly"]="true"}},
		{"extra volume",func(_ *ProjectServiceAttachment,f *projectServiceInspectionFake){f.instance.Devices["other"]=map[string]string{"type":"disk","pool":"x","source":"y","path":"/other"}}},
		{"foreign volume marker",func(_ *ProjectServiceAttachment,f *projectServiceInspectionFake){f.volume.Config["user.aginctus.resource"]="foreign"}},
		{"shared volume",func(_ *ProjectServiceAttachment,f *projectServiceInspectionFake){f.volume.UsedBy=append(f.volume.UsedBy,"/1.0/instances/foreign?project=dev-team")}},
		{"wrong project attachment",func(_ *ProjectServiceAttachment,f *projectServiceInspectionFake){f.volume.UsedBy[0]="/1.0/instances/state-service?project=foreign"}},
		{"unsupported implicit project",func(_ *ProjectServiceAttachment,f *projectServiceInspectionFake){f.volume.UsedBy[0]="/1.0/instances/state-service"}},
		{"no etag",func(_ *ProjectServiceAttachment,f *projectServiceInspectionFake){f.volumeETag=""}},
		{"invalid requested root mount",func(w *ProjectServiceAttachment,_ *projectServiceInspectionFake){w.MountPath="/"}},
	} {
		t.Run(tc.name,func(t *testing.T){
			want,f:=projectAttachmentFixture()
			tc.change(&want,f)
			if err:=VerifyProjectServiceAttachment(context.Background(),f,want);err==nil {
				t.Fatal("accepted unverified project state volume attachment")
			}
		})
	}
}
