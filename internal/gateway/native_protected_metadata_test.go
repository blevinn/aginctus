package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	"github.com/blevinn/aginctus/internal/orchestration/nativeoci"
)
func TestProtectedRuntimeMetadataIsNarrowAndPinned(t *testing.T){
	spec:=environment.GatewayImageSpec{Registry:"docker.io",Repository:"library/postgres",ManifestDigest:"sha256:"+strings.Repeat("a",64),ConfigDigest:"sha256:"+strings.Repeat("b",64),IncusFingerprint:strings.Repeat("c",64),Alias:"postgres",Architecture:"amd64"}
	g:=environment.ProtectedGatewayGrant{ProtectedProject:"protected"}
	e:=native.GatewayImageEvidence{Project:g.ProtectedProject,Alias:spec.Alias,AliasMarker:newGatewayAliasMarker(),Identity:native.GatewayOCIIdentity{Registry:spec.Registry,Repository:spec.Repository,ManifestDigest:spec.ManifestDigest,ConfigDigest:spec.ConfigDigest,IncusFingerprint:spec.IncusFingerprint}}
	i:=nativeoci.ResolvedImage{Registry:spec.Registry,Repository:spec.Repository,ManifestDigest:spec.ManifestDigest,ConfigDigest:spec.ConfigDigest,IncusFingerprint:spec.IncusFingerprint}
	i.Config.Entrypoint=[]string{"docker-entrypoint.sh"}
	i.Config.Cmd=[]string{"postgres"}
	i.Config.Env=[]string{"PG_MAJOR=17","PGDATA=/var/lib/postgresql/data","UNRELATED=synthetic-secret-marker"}
	m,err:=projectRuntimeMetadata(g,"postgres",spec,e,i)
	if err!=nil{t.Fatal(err)}
	raw,err:=json.Marshal(m)
	if err!=nil||strings.Contains(string(raw),"synthetic-secret-marker"){t.Fatal("unrelated config retained")}
	if _,err:=m.resolve(g,"postgres",spec,e);err!=nil{t.Fatal(err)}
	changed:=spec;changed.Architecture="arm64"
	if _,err:=m.resolve(g,"postgres",changed,e);err==nil{t.Fatal("different platform accepted")}
	changedEvidence:=e;changedEvidence.AliasMarker=newGatewayAliasMarker()
	if _,err:=m.resolve(g,"postgres",spec,changedEvidence);err==nil{t.Fatal("different acquisition accepted")}
	if _,err:=m.resolve(g,"litellm",spec,e);err==nil{t.Fatal("different role accepted")}
}
