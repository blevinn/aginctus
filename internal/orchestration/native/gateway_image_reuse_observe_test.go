package native

import (
	"context"
	"errors"
	"strings"
	"testing"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

type fakeGatewayReuseReader struct {
	info *incus.ConnectionInfo
	project *api.Project
	alias *api.ImageAliasesEntry
	image *api.Image
	reads int
}
func (f *fakeGatewayReuseReader) GetConnectionInfo()(*incus.ConnectionInfo,error){return f.info,nil}
func (f *fakeGatewayReuseReader) GetProject(string)(*api.Project,string,error){f.reads++;return f.project,"",nil}
func (f *fakeGatewayReuseReader) GetImageAlias(string)(*api.ImageAliasesEntry,string,error){f.reads++;return f.alias,"",nil}
func (f *fakeGatewayReuseReader) GetImage(string)(*api.Image,string,error){f.reads++;return f.image,"",nil}
func reuseReaderFixture()(GatewayImageAcquireIntent,string,GatewayImageEvidence,*fakeGatewayReuseReader){
	want,_:=imageAcquireFixture()
	aliasName:="postgres-image"
	evidence:=GatewayImageEvidence{Project:want.Project,Alias:aliasName,Identity:want.Image}
	scope,_:=incusscope.NewProjectScope(want.Project)
	r:=&fakeGatewayReuseReader{
		info:&incus.ConnectionInfo{Protocol:"incus",SocketPath:"/run/incus/unix.socket",Project:want.Project},
		project:&api.Project{Name:want.Project,ProjectPut:api.ProjectPut{Config:scope.DesiredConfig()}},
		alias:&api.ImageAliasesEntry{Name:aliasName,ImageAliasesEntryPut:api.ImageAliasesEntryPut{Target:want.Image.IncusFingerprint}},
		image:&api.Image{Project:want.Project,Type:"container",Fingerprint:want.Image.IncusFingerprint},
	}
	return want,aliasName,evidence,r
}
func TestObserveGatewayImageReuseChecksSDKState(t *testing.T){
	want,alias,evidence,reader:=reuseReaderFixture()
	if err:=ObserveGatewayImageReuseWithClient(context.Background(),want,alias,evidence,reader);err!=nil {t.Fatal(err)}
	if reader.reads!=3 {t.Fatalf("reads=%d",reader.reads)}
}
func TestObserveGatewayImageReuseRejectsUnknownAndForeign(t *testing.T){
	tests:=map[string]func(*GatewayImageEvidence,*fakeGatewayReuseReader){
		"missing project":func(_ *GatewayImageEvidence,r *fakeGatewayReuseReader){r.project=nil},
		"wrong selected project":func(_ *GatewayImageEvidence,r *fakeGatewayReuseReader){r.info.Project="default"},
		"missing alias":func(_ *GatewayImageEvidence,r *fakeGatewayReuseReader){r.alias=nil},
		"wrong alias target":func(_ *GatewayImageEvidence,r *fakeGatewayReuseReader){r.alias.Target=strings.Repeat("c",64)},
		"missing image":func(_ *GatewayImageEvidence,r *fakeGatewayReuseReader){r.image=nil},
		"foreign image":func(_ *GatewayImageEvidence,r *fakeGatewayReuseReader){r.image.Project="foreign"},
		"config digest drift":func(e *GatewayImageEvidence,_ *fakeGatewayReuseReader){e.Identity.ConfigDigest="sha256:"+strings.Repeat("d",64)},
	}
	for name,change:=range tests{
		t.Run(name,func(t *testing.T){
			want,alias,evidence,reader:=reuseReaderFixture()
			change(&evidence,reader)
			if err:=ObserveGatewayImageReuseWithClient(context.Background(),want,alias,evidence,reader);err==nil {t.Fatal("invalid image identity accepted")}
		})
	}
}
func TestObserveGatewayImageReuseCancellationBeforeRead(t *testing.T){
	want,alias,evidence,reader:=reuseReaderFixture()
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	err:=ObserveGatewayImageReuseWithClient(ctx,want,alias,evidence,reader)
	if !errors.Is(err,context.Canceled) || reader.reads!=0 {t.Fatalf("cancel: %v reads=%d",err,reader.reads)}
}
