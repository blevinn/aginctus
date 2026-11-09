package native

import (
	"context"
	"errors"
	"net/http"
	"testing"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

type fakeGatewayVolumeClient struct {
	info *incus.ConnectionInfo
	project *api.Project
	volume *api.StorageVolume
	getErr error
	creates int
	onCreate func()
}

func (f *fakeGatewayVolumeClient) GetConnectionInfo() (*incus.ConnectionInfo,error) { return f.info,nil }
func (f *fakeGatewayVolumeClient) GetProject(string) (*api.Project,string,error) { return f.project,"",nil }
func (f *fakeGatewayVolumeClient) GetStoragePoolVolume(_,_,_ string) (*api.StorageVolume,string,error) {
	if f.getErr != nil { return nil,"",f.getErr }
	if f.volume == nil { return nil,"",api.StatusErrorf(http.StatusNotFound,"absent") }
	// The mock's nil response models a denied/unknown lookup, never absence.
	return f.volume,"",nil
}
func (f *fakeGatewayVolumeClient) CreateStoragePoolVolume(_ string, req api.StorageVolumesPost) error {
	f.creates++
	f.volume = &api.StorageVolume{Name:req.Name,Type:req.Type,ContentType:req.ContentType,StorageVolumePut:req.StorageVolumePut}
	if f.onCreate != nil { f.onCreate() }
	return nil
}

func gatewayVolumeFixture() (GatewayVolumeIntent,*fakeGatewayVolumeClient) {
	intent := GatewayVolumeIntent{Project:"aginctus-test",Pool:"pool",Name:"gateway-postgres"}
	scope,_ := incusscope.NewProjectScope(intent.Project)
	return intent,&fakeGatewayVolumeClient{
		info:&incus.ConnectionInfo{Protocol:"incus",SocketPath:"/run/incus/unix.socket",Project:intent.Project},
		project:&api.Project{Name:intent.Project,ProjectPut:api.ProjectPut{Config:scope.DesiredConfig()}},
	}
}
func ownedGatewayVolume(v GatewayVolumeIntent) *api.StorageVolume {
	return &api.StorageVolume{Name:v.Name,Type:"custom",ContentType:"filesystem",
		StorageVolumePut:api.StorageVolumePut{Config:v.markers()}}
}
func TestGatewayVolumeOwnedReuse(t *testing.T) {
	v,f:=gatewayVolumeFixture()
	f.volume=ownedGatewayVolume(v)
	r,err:=EnsureGatewayVolumeWithClient(context.Background(),v,f)
	if err!=nil || !r.Verified || r.Created || f.creates!=0 { t.Fatalf("reuse: %+v %v creates=%d",r,err,f.creates) }
}
func TestGatewayVolumeFailsClosedOnUnknownAndForeign(t *testing.T) {
	v,f:=gatewayVolumeFixture()
	f.getErr=api.StatusErrorf(http.StatusForbidden,"denied")
	if _,err:=EnsureGatewayVolumeWithClient(context.Background(),v,f);err==nil || f.creates!=0 {
		t.Fatalf("unknown volume accepted: %v",err)
	}
	f.getErr=nil
	f.volume=ownedGatewayVolume(v)
	delete(f.volume.Config,"user.aginctus.managed")
	if _,err:=EnsureGatewayVolumeWithClient(context.Background(),v,f);err==nil || f.creates!=0 {
		t.Fatalf("foreign volume accepted: %v",err)
	}
}
func TestGatewayVolumeRequiresLocalProject(t *testing.T) {
	v,f:=gatewayVolumeFixture()
	f.info.Project="default"
	if _,err:=EnsureGatewayVolumeWithClient(context.Background(),v,f);err==nil || f.creates!=0 {
		t.Fatalf("wrong project accepted: %v",err)
	}
}
func TestGatewayVolumeCancellation(t *testing.T) {
	v,f:=gatewayVolumeFixture()
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	_,err:=EnsureGatewayVolumeWithClient(ctx,v,f)
	if !errors.Is(err,context.Canceled) || f.creates!=0 { t.Fatalf("cancel: %v",err) }
}

func TestGatewayVolumeCreatesOnlyAbsent(t *testing.T) {
	v,f:=gatewayVolumeFixture()
	r,err:=EnsureGatewayVolumeWithClient(context.Background(),v,f)
	if err!=nil || !r.Created || !r.Verified || f.creates!=1 { t.Fatalf("create: %+v %v creates=%d",r,err,f.creates) }
	r,err=EnsureGatewayVolumeWithClient(context.Background(),v,f)
	if err!=nil || r.Created || !r.Verified || f.creates!=1 { t.Fatalf("repeat: %+v %v creates=%d",r,err,f.creates) }
}
func TestGatewayVolumeUncertainAfterCreate(t *testing.T) {
	v,f:=gatewayVolumeFixture()
	f.onCreate=func(){f.volume=nil}
	r,err:=EnsureGatewayVolumeWithClient(context.Background(),v,f)
	var typed *GatewayVolumeError
	if !errors.As(err,&typed) || !typed.Attempted || !typed.Uncertain ||
		!r.Created || r.Verified || f.creates!=1 { t.Fatalf("uncertain completion: %+v %v",r,err) }
}
