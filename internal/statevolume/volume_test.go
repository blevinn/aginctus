package statevolume

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lxc/incus/v7/shared/api"
)
type mockBackend struct{
	volume *api.StorageVolume
	getError error
	createError error
	creates int
}
func (m *mockBackend) Get(_ context.Context, _, name string) (*api.StorageVolume,error) {
	if m.getError!=nil {return nil,m.getError}
	return m.volume,nil
}
func (m *mockBackend) Create(_ context.Context,_ string,req api.StorageVolumesPost) error {
	m.creates++
	if m.createError!=nil{return m.createError}
	m.volume=&api.StorageVolume{
		Name:req.Name,Type:req.Type,ContentType:req.ContentType,
		StorageVolumePut:req.StorageVolumePut,
	}
	m.getError=nil
	return nil
}
func TestEnsureCreatesMissingAndPreservesOwnedVolume(t *testing.T){
	v:=Volume{Pool:"default",Instance:"aginctus-herdr",Class:"system"}
	m:=&mockBackend{getError:NotFound{Name:v.Name()}}
	if err:=Ensure(context.Background(),m,v);err!=nil{t.Fatal(err)}
	if m.creates!=1 {t.Fatalf("creates=%d",m.creates)}
	if err:=Ensure(context.Background(),m,v);err!=nil{t.Fatal(err)}
	if m.creates!=1 {t.Fatal("existing volume recreated")}
	if m.volume.Config["user.aginctus.instance"]!="aginctus-herdr"{t.Fatal("missing ownership markers")}
}
func TestEnsureRejectsForeignOrWrongClass(t *testing.T){
	v:=Volume{Pool:"default",Instance:"aginctus-herdr",Class:"system"}
	for _,tc:=range []struct{name string;volume *api.StorageVolume}{
		{"unmarked", &api.StorageVolume{Name:v.Name(),Type:"custom",ContentType:"filesystem"}},
		{"wrong-content", &api.StorageVolume{Name:v.Name(),Type:"custom",ContentType:"block"}},
		{"wrong-name", &api.StorageVolume{Name:"other",Type:"custom",ContentType:"filesystem"}},
	}{
		t.Run(tc.name,func(t *testing.T){
			m:=&mockBackend{volume:tc.volume}
			err:=Ensure(context.Background(),m,v)
			if err==nil||m.creates!=0{t.Fatalf("expected rejection: %v creates=%d",err,m.creates)}
		})
	}
}
func TestEnsureDoesNotMaskLookupOrCreateErrors(t *testing.T){
	v:=Volume{Pool:"default",Instance:"aginctus-herdr",Class:"system"}
	m:=&mockBackend{getError:errors.New("denied")}
	if err:=Ensure(context.Background(),m,v);err==nil||m.creates!=0{t.Fatal("unexpected create on lookup failure")}
	m.getError=NotFound{Name:v.Name()}
	m.createError=errors.New("no capacity")
	if err:=Ensure(context.Background(),m,v);err==nil||!strings.Contains(err.Error(),"no capacity"){t.Fatalf("err=%v",err)}
}
func TestVolumeRejectsInvalidNamesAndClasses(t *testing.T){
	for _,v:=range []Volume{
		{Pool:"default",Instance:"../bad",Class:"system"},
		{Pool:"default",Instance:"aginctus-dev",Class:"secret"},
		{Pool:"",Instance:"aginctus-dev",Class:"system"},
	}{
		if err:=v.Validate();err==nil{t.Fatalf("accepted %#v",v)}
	}
}
