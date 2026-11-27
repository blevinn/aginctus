package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/blevinn/aginctus/internal/filestate"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	"github.com/blevinn/aginctus/internal/orchestration/nativeoci"
)

// Store only the runtime facts consumed by the reviewed launchers. The
// administrator-owned acquisition record is the trust root; this projection
// is not an independent signature of the registry response. Never persist
// arbitrary OCI environment variables, labels, health commands or config.
type protectedRuntimeMetadata struct {
	Version int `json:"version"`
	Grant environment.ProtectedGatewayGrant `json:"grant"`
	Role string `json:"role"`
	Spec environment.GatewayImageSpec `json:"spec"`
	Evidence native.GatewayImageEvidence `json:"evidence"`
	User string `json:"user"`
	WorkingDir string `json:"workingDir"`
	Postgres17 bool `json:"postgres17"`
}

func projectRuntimeMetadata(g environment.ProtectedGatewayGrant, role string, spec environment.GatewayImageSpec, evidence native.GatewayImageEvidence, image nativeoci.ResolvedImage) (protectedRuntimeMetadata, error) {
	m := protectedRuntimeMetadata{Version:1, Grant:g, Role:role, Spec:spec, Evidence:evidence}
	if image.Registry != spec.Registry || image.Repository != spec.Repository || image.ManifestDigest != spec.ManifestDigest ||
		image.ConfigDigest != spec.ConfigDigest || image.IncusFingerprint != spec.IncusFingerprint {
		return protectedRuntimeMetadata{}, errProtectedGateway
	}
	switch role {
	case "postgres":
		if gatewayImageLauncher(role,image)==nil || !slices.Contains(image.Config.Env,"PG_MAJOR=17") ||
			!slices.Contains(image.Config.Env,"PGDATA=/var/lib/postgresql/data") {
			return protectedRuntimeMetadata{}, errProtectedGateway
		}
		m.Postgres17 = true
		m.WorkingDir = image.Config.WorkingDir
	case "litellm":
		if gatewayImageLauncher(role,image)==nil || !liteLLMRootFileIdentity(image) {
			return protectedRuntimeMetadata{}, errProtectedGateway
		}
		m.User = image.Config.User
	default:
		return protectedRuntimeMetadata{}, errProtectedGateway
	}
	return m,nil
}

func (m protectedRuntimeMetadata) resolve(g environment.ProtectedGatewayGrant, role string, spec environment.GatewayImageSpec, evidence native.GatewayImageEvidence) (nativeoci.ResolvedImage,error) {
	if m.Version!=1 || m.Grant!=g || m.Role!=role || m.Spec!=spec || m.Evidence!=evidence ||
		evidence.Project!=g.ProtectedProject || evidence.Alias!=spec.Alias || evidence.AliasMarker=="" ||
		evidence.Validate()!=nil || evidence.Identity!=(native.GatewayOCIIdentity{Registry:spec.Registry,Repository:spec.Repository,ManifestDigest:spec.ManifestDigest,ConfigDigest:spec.ConfigDigest,IncusFingerprint:spec.IncusFingerprint}) {
		return nativeoci.ResolvedImage{},errProtectedGateway
	}
	i := nativeoci.ResolvedImage{Registry:spec.Registry, Repository:spec.Repository, ManifestDigest:spec.ManifestDigest, ConfigDigest:spec.ConfigDigest, IncusFingerprint:spec.IncusFingerprint}
	i.Config.User=m.User
	i.Config.WorkingDir=m.WorkingDir
	switch role {
	case "postgres":
		if !m.Postgres17 || m.User!="" || (m.WorkingDir!="" && m.WorkingDir!="/") {return nativeoci.ResolvedImage{},errProtectedGateway}
		i.Config.Entrypoint=[]string{"docker-entrypoint.sh"}
		i.Config.Cmd=[]string{"postgres"}
		i.Config.Env=[]string{"PG_MAJOR=17","PGDATA=/var/lib/postgresql/data"}
	case "litellm":
		if m.Postgres17 || m.WorkingDir!="" || !liteLLMRootFileIdentity(i) || gatewayImageLauncher(role,i)==nil {return nativeoci.ResolvedImage{},errProtectedGateway}
	default:
		return nativeoci.ResolvedImage{},errProtectedGateway
	}
	return i,nil
}

func (e *protectedExecution) runtimeMetadataPath(role string) string {
	return filepath.Join(e.provenance.directory,e.revision.ID+"-"+role+"-runtime.json")
}

func (e *protectedExecution) readRuntimeMetadata(role string, spec environment.GatewayImageSpec, evidence native.GatewayImageEvidence) (nativeoci.ResolvedImage,error) {
	raw,err:=filestate.ReadPrivateFile(e.runtimeMetadataPath(role),8192)
	if err!=nil{return nativeoci.ResolvedImage{},err}
	var m protectedRuntimeMetadata
	if json.Unmarshal(raw,&m)!=nil{return nativeoci.ResolvedImage{},errProtectedGateway}
	canonical,err:=json.Marshal(m)
	if err!=nil||!bytes.Equal(raw,canonical){return nativeoci.ResolvedImage{},errProtectedGateway}
	return m.resolve(e.grant,role,spec,evidence)
}

func (e *protectedExecution) stageRuntimeMetadata(ctx context.Context) error {
	for index,spec:=range []environment.GatewayImageSpec{e.candidate.Gateway.Postgres,e.candidate.Gateway.LiteLLM} {
		role:=gatewayRoles[index]
		if e.guard(ctx)!=nil{return errProtectedGateway}
		evidence,err:=e.provenance.ExistingImageEvidence(ctx,e.revision,role)
		if err!=nil||native.ObserveGatewayImageReuseWithClient(ctx,native.GatewayImageAcquireIntent{Project:e.grant.ProtectedProject,Image:evidence.Identity},spec.Alias,evidence,e.sdk)!=nil{return errProtectedGateway}
		if _,err:=e.readRuntimeMetadata(role,spec,evidence);err==nil{continue}else if !errors.Is(err,os.ErrNotExist){return errProtectedGateway}
		image,err:=nativeoci.ResolvePublic(ctx,spec.Source())
		if err!=nil{return errProtectedGateway}
		m,err:=projectRuntimeMetadata(e.grant,role,spec,evidence,image)
		if err!=nil{return err}
		if _,err:=m.resolve(e.grant,role,spec,evidence);err!=nil{return err}
		raw,err:=json.Marshal(m)
		if err!=nil||len(raw)>8192||e.guard(ctx)!=nil{return errProtectedGateway}
		// An immutable local observation, not an Incus effect. An interrupted
		// write never permits startup; refuse contradictory existing evidence.
		file,err:=os.OpenFile(e.runtimeMetadataPath(role),os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600)
		if err!=nil{return errProtectedGateway}
		_,werr:=file.Write(raw)
		if errors.Join(werr,file.Sync(),file.Close())!=nil{return errProtectedGateway}
		dir,err:=os.Open(e.provenance.directory)
		if err!=nil{return errProtectedGateway}
		if errors.Join(dir.Sync(),dir.Close())!=nil{return errProtectedGateway}
		if _,err:=e.readRuntimeMetadata(role,spec,evidence);err!=nil{return errProtectedGateway}
	}
	return e.guard(ctx)
}

// StageLocalProtectedGatewayRuntimeMetadata explicitly finishes online
// staging before the offline delivery/start boundary. It performs no Incus
// mutation and uses the existing exact provisioning grant and writer pin.
func StageLocalProtectedGatewayRuntimeMetadata(ctx context.Context,grantPath,operatorPath string)(id string,err error){
	e,err:=openProtectedExecution(ctx,grantPath,operatorPath,"gateway-provision")
	if err!=nil{return "",err}
	id=e.session.ID()
	defer func(){err=errors.Join(err,e.finish(ctx,err==nil))}()
	return id,e.stageRuntimeMetadata(ctx)
}
