package native

import (
	"strings"
	"testing"

	"github.com/lxc/incus/v7/shared/api"
)

func TestAssessGatewayImageReuseRequiresFullIdentityEvidence(t *testing.T) {
	intent,_:=imageAcquireFixture()
	aliasName:="aginctus-postgres"
	evidence:=GatewayImageEvidence{Project:intent.Project,Alias:aliasName,Identity:intent.Image}
	alias:=&api.ImageAliasesEntry{Name:aliasName,ImageAliasesEntryPut:api.ImageAliasesEntryPut{Target:intent.Image.IncusFingerprint}}
	image:=&api.Image{Project:intent.Project,Type:"container",Fingerprint:intent.Image.IncusFingerprint}
	if err:=AssessGatewayImageReuse(intent,aliasName,evidence,alias,image);err!=nil {t.Fatal(err)}
	tests:=map[string]func(*GatewayImageEvidence,*api.ImageAliasesEntry,*api.Image) {
		"config-only collision":func(e *GatewayImageEvidence,_ *api.ImageAliasesEntry,_ *api.Image){e.Identity.ConfigDigest="sha256:"+strings.Repeat("d",64)},
		"manifest collision":func(e *GatewayImageEvidence,_ *api.ImageAliasesEntry,_ *api.Image){e.Identity.ManifestDigest="sha256:"+strings.Repeat("d",64)},
		"wrong project evidence":func(e *GatewayImageEvidence,_ *api.ImageAliasesEntry,_ *api.Image){e.Project="foreign"},
		"wrong alias evidence":func(e *GatewayImageEvidence,_ *api.ImageAliasesEntry,_ *api.Image){e.Alias="other"},
		"alias redirect":func(_ *GatewayImageEvidence,a *api.ImageAliasesEntry,_ *api.Image){a.Target=strings.Repeat("d",64)},
		"foreign image":func(_ *GatewayImageEvidence,_ *api.ImageAliasesEntry,i *api.Image){i.Project="foreign"},
		"wrong image type":func(_ *GatewayImageEvidence,_ *api.ImageAliasesEntry,i *api.Image){i.Type="virtual-machine"},
	}
	for name,change:=range tests {
		t.Run(name,func(t *testing.T){
			e:=evidence;a:=*alias;i:=*image
			change(&e,&a,&i)
			if err:=AssessGatewayImageReuse(intent,aliasName,e,&a,&i);err==nil {t.Fatal("unsafe image reused")}
		})
	}
	for _,tc:=range []struct{alias *api.ImageAliasesEntry;image *api.Image}{{nil,image},{alias,nil}} {
		if err:=AssessGatewayImageReuse(intent,aliasName,evidence,tc.alias,tc.image);err==nil {t.Fatal("missing object accepted")}
	}
}
