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
type fakeGatewayAliasClient struct {
	info *incus.ConnectionInfo
	project *api.Project
	image *api.Image
	alias *api.ImageAliasesEntry
	creates int
	createErr error
	onCreate func()
}
func (f *fakeGatewayAliasClient) GetConnectionInfo()(*incus.ConnectionInfo,error){return f.info,nil}
func (f *fakeGatewayAliasClient) GetProject(string)(*api.Project,string,error){return f.project,"",nil}
func (f *fakeGatewayAliasClient) GetImage(string)(*api.Image,string,error){return f.image,"",nil}
func (f *fakeGatewayAliasClient) GetImageAlias(string)(*api.ImageAliasesEntry,string,error){
	if f.alias==nil{return nil,"",api.StatusErrorf(http.StatusNotFound,"absent")}
	return f.alias,"",nil
}
func (f *fakeGatewayAliasClient) CreateImageAlias(req api.ImageAliasesPost)error{
	f.creates++
	if f.createErr!=nil{return f.createErr}
	f.alias=&req.ImageAliasesEntry
	if f.onCreate!=nil {f.onCreate()}
	return nil
}
func gatewayAliasFixture()(GatewayAliasCreateIntent,*fakeGatewayAliasClient){
	intent,_:=imageAcquireFixture()
	in:=GatewayAliasCreateIntent{
		Project:intent.Project,Alias:"postgres-image",Image:intent.Image,
		Acquisition:GatewayImageAcquireResult{Project:intent.Project,
			Fingerprint:intent.Image.IncusFingerprint,Attempted:true,Verified:true},
	}
	scope,_:=incusscope.NewProjectScope(intent.Project)
	return in,&fakeGatewayAliasClient{
		info:&incus.ConnectionInfo{Protocol:"incus",SocketPath:"/run/incus/unix.socket",Project:intent.Project},
		project:&api.Project{Name:intent.Project,ProjectPut:api.ProjectPut{Config:scope.DesiredConfig()}},
		image:&api.Image{Project:intent.Project,Type:"container",Fingerprint:intent.Image.IncusFingerprint},
	}
}
func TestGatewayAliasCreatesAndVerifies(t *testing.T){
	in,f:=gatewayAliasFixture()
	out,err:=CreateGatewayImageAliasWithClient(context.Background(),in,f)
	if err!=nil || !out.Created || !out.Verified || f.creates!=1 {t.Fatalf("create: %+v %v",out,err)}
	if _,err:=CreateGatewayImageAliasWithClient(context.Background(),in,f);err==nil || f.creates!=1 {
		t.Fatal("repeated create adopted existing alias")
	}
}
func TestGatewayAliasRejectsUntrustedInputsBeforeMutation(t *testing.T){
	tests:=map[string]func(*GatewayAliasCreateIntent,*fakeGatewayAliasClient) {
		"unverified acquisition":func(i *GatewayAliasCreateIntent,_ *fakeGatewayAliasClient){i.Acquisition.Verified=false},
		"unattempted acquisition":func(i *GatewayAliasCreateIntent,_ *fakeGatewayAliasClient){i.Acquisition.Attempted=false},
		"wrong project":func(_ *GatewayAliasCreateIntent,f *fakeGatewayAliasClient){f.info.Project="default"},
		"foreign image":func(_ *GatewayAliasCreateIntent,f *fakeGatewayAliasClient){f.image.Project="foreign"},
		"foreign project policy":func(_ *GatewayAliasCreateIntent,f *fakeGatewayAliasClient){f.project.Config["user.aginctus.managed"]="false"},
		"existing alias":func(i *GatewayAliasCreateIntent,f *fakeGatewayAliasClient){f.alias=&api.ImageAliasesEntry{Name:i.Alias}},
	}
	for name,change:=range tests{
		t.Run(name,func(t *testing.T){
			in,f:=gatewayAliasFixture()
			change(&in,f)
			if _,err:=CreateGatewayImageAliasWithClient(context.Background(),in,f);err==nil || f.creates!=0 {
				t.Fatalf("unsafe alias write: %v",err)
			}
		})
	}
}
func TestGatewayAliasUncertainAfterSubmission(t *testing.T){
	in,f:=gatewayAliasFixture()
	f.onCreate=func(){f.alias=nil}
	out,err:=CreateGatewayImageAliasWithClient(context.Background(),in,f)
	var typed *GatewayAliasCreateError
	if !errors.As(err,&typed) || !typed.Uncertain || !typed.Attempted || !out.Created || out.Verified {
		t.Fatalf("uncertain: %+v %v",out,err)
	}
}
func TestGatewayAliasCancellation(t *testing.T){
	in,f:=gatewayAliasFixture()
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	_,err:=CreateGatewayImageAliasWithClient(ctx,in,f)
	if !errors.Is(err,context.Canceled) || f.creates!=0 {t.Fatalf("cancel: %v",err)}
}

func TestGatewayAliasRejectsChangedTargetAfterCreate(t *testing.T) {
	in,f:=gatewayAliasFixture()
	f.onCreate=func(){f.image.Project="foreign"}
	result,err:=CreateGatewayImageAliasWithClient(context.Background(),in,f)
	var typed *GatewayAliasCreateError
	if !errors.As(err,&typed) || !typed.Uncertain || !typed.Attempted ||
		!result.Created || result.Verified {
		t.Fatalf("changed image postflight: %+v %v",result,err)
	}
}
