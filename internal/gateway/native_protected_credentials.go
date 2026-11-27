package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"os"

	"github.com/blevinn/aginctus/internal/filestate"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	"github.com/blevinn/aginctus/internal/orchestration/nativeoci"
)

type protectedCredentialFile struct {
	APIVersion string `json:"apiVersion"`
	Grant environment.ProtectedGatewayGrant `json:"grant"`
	Version string `json:"version"`
	PostgresPassword string `json:"postgresPassword"`
	MasterKey string `json:"masterKey"`
	SaltKey string `json:"saltKey"`
}

// The secret source is fixed beneath the independently provisioned protected
// directory, never a candidate path or environment variable. Require canonical
// JSON to reject duplicate and aliased fields. Never return parser diagnostics.
func parseProtectedCredentialFile(raw []byte,g environment.ProtectedGatewayGrant,version string)(GatewayCredentialMaterial,error){
	var record protectedCredentialFile
	if json.Unmarshal(raw,&record)!=nil || record.APIVersion!="aginctus.dev/protected-gateway-credentials/v1alpha1" ||
		record.Grant!=g || record.Version!=version {
		return GatewayCredentialMaterial{},errCredentialDelivery
	}
	canonical,err:=json.Marshal(record)
	if err!=nil||!bytes.Equal(bytes.TrimSpace(raw),canonical){return GatewayCredentialMaterial{},errCredentialDelivery}
	return NewGatewayCredentialMaterial(record.Version,record.PostgresPassword,record.MasterKey,record.SaltKey)
}

func (e *protectedExecution) credentialMaterial()(GatewayCredentialMaterial,error){
	path:=filepath.Join(e.provenance.directory,"credentials.json")
	if !trustedGatewayPath(filepath.Dir(path)){return GatewayCredentialMaterial{},errCredentialDelivery}
	raw,err:=filestate.ReadPrivateFile(path,8192)
	if err!=nil{return GatewayCredentialMaterial{},errCredentialDelivery}
	defer clear(raw)
	return parseProtectedCredentialFile(raw,e.grant,e.candidate.Gateway.CredentialVersion)
}

func (e *protectedExecution) pinnedImages(ctx context.Context)(NativeGatewayCreateImages,[2]nativeoci.ResolvedImage,error){
	var images NativeGatewayCreateImages
	var resolved [2]nativeoci.ResolvedImage
	out:=[]*NativeGatewayCreateImage{&images.Postgres,&images.LiteLLM}
	for i,spec:=range []environment.GatewayImageSpec{e.candidate.Gateway.Postgres,e.candidate.Gateway.LiteLLM}{
		if e.guard(ctx)!=nil{return NativeGatewayCreateImages{},resolved,errProtectedGateway}
		want:=native.GatewayOCIIdentity{Registry:spec.Registry,Repository:spec.Repository,ManifestDigest:spec.ManifestDigest,ConfigDigest:spec.ConfigDigest,IncusFingerprint:spec.IncusFingerprint}
		evidence,err:=e.provenance.ExistingImageEvidence(ctx,e.revision,gatewayRoles[i])
		if err!=nil||native.ObserveGatewayImageReuseWithClient(ctx,native.GatewayImageAcquireIntent{Project:e.grant.ProtectedProject,Image:want},spec.Alias,evidence,e.sdk)!=nil{
			return NativeGatewayCreateImages{},resolved,errProtectedGateway
		}
		image,err:=nativeoci.ResolvePublic(ctx,spec.Source())
		if err!=nil||image.Registry!=want.Registry||image.Repository!=want.Repository||image.ManifestDigest!=want.ManifestDigest||image.ConfigDigest!=want.ConfigDigest||image.IncusFingerprint!=want.IncusFingerprint{
			return NativeGatewayCreateImages{},resolved,errProtectedGateway
		}
		*out[i]=NativeGatewayCreateImage{Expected:want,Evidence:evidence}
		resolved[i]=image
	}
	return images,resolved,e.guard(ctx)
}

// PublishLocalProtectedGatewayCredentials is a direct root-only installed
// publisher, using exact accepted intent, independent root-owned secret source,
// durable provisioning evidence and the live state-service execution pin.
func PublishLocalProtectedGatewayCredentials(ctx context.Context,grantPath,operatorPath string)(receipt GatewayCredentialDeliveryReceipt,id string,err error){
	e,err:=openProtectedExecution(ctx,grantPath,operatorPath,"gateway-deliver")
	if err!=nil{return receipt,"",err}
	id=e.session.ID()
	defer func(){err=errors.Join(err,e.finish(ctx,err==nil))}()
	material,err:=e.credentialMaterial()
	if err!=nil{return receipt,id,errCredentialDelivery}
	images,resolved,err:=e.pinnedImages(ctx)
	if err!=nil{return receipt,id,errProtectedGateway}
	guard:=func(ctx context.Context)error{
		if e.guard(ctx)!=nil{return errProtectedGateway}
		current,err:=e.credentialMaterial()
		if err!=nil||current!=material{return errCredentialDelivery}
		return ctx.Err()
	}
	if guard(ctx)!=nil{return receipt,id,errCredentialDelivery}
	if prior,readErr:=e.deliveryReceipt();readErr==nil {
		if e.verifyDelivery(ctx,prior,material,images)!=nil{return receipt,id,errCredentialDelivery}
		return prior,id,nil
	} else if !errors.Is(readErr,os.ErrNotExist){return receipt,id,errCredentialDelivery}
	if e.session.BeginEffect(ctx,"deliver")!=nil{return receipt,id,errProtectedGateway}
	receipt,err=StageStoppedGatewayCredentialFiles(ctx,e.revision,e.candidate,e.plan.top,e.plan.devices,e.plan.pair,images,resolved[0],resolved[1],material,e.sdk,guard)
	if err!=nil{return receipt,id,errCredentialDelivery}
	if guard(ctx)!=nil||e.saveDeliveryReceipt(receipt)!=nil||e.session.MarkApplied(ctx)!=nil{return receipt,id,errCredentialDelivery}
	return receipt,id,nil
}

func (e *protectedExecution) deliveryReceipt()(GatewayCredentialDeliveryReceipt,error){
	var receipt GatewayCredentialDeliveryReceipt
	path:=filepath.Join(e.provenance.directory,"delivery.json")
	raw,err:=filestate.ReadPrivateFile(path,8192)
	if err!=nil{return receipt,err}
	if json.Unmarshal(raw,&receipt)!=nil{return receipt,errCredentialDelivery}
	canonical,err:=json.Marshal(receipt)
	if err!=nil||!bytes.Equal(raw,canonical)||!receipt.Verified||receipt.Revision!=e.revision||
		receipt.PhysicalProject!=e.grant.ProtectedProject||receipt.Version!=e.candidate.Gateway.CredentialVersion||
		receipt.PostgresCreatedAt.IsZero()||receipt.LiteLLMCreatedAt.IsZero(){return GatewayCredentialDeliveryReceipt{},errCredentialDelivery}
	return receipt,nil
}
func (e *protectedExecution) saveDeliveryReceipt(r GatewayCredentialDeliveryReceipt)error{
	path:=filepath.Join(e.provenance.directory,"delivery.json")
	raw,err:=json.Marshal(r);if err!=nil{return errCredentialDelivery}
	file,err:=os.OpenFile(path,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);if err!=nil{return errCredentialDelivery}
	_,werr:=file.Write(raw);serr:=file.Sync();cerr:=file.Close()
	if errors.Join(werr,serr,cerr)!=nil{return errCredentialDelivery}
	dir,err:=os.Open(e.provenance.directory);if err!=nil{return errCredentialDelivery}
	defer dir.Close()
	return dir.Sync()
}
func (e *protectedExecution) verifyDelivery(ctx context.Context,r GatewayCredentialDeliveryReceipt,m GatewayCredentialMaterial,images NativeGatewayCreateImages)error{
	if e.guard(ctx)!=nil||r.Revision!=e.revision||r.PhysicalProject!=e.grant.ProtectedProject||r.Version!=m.version||!r.Verified{return errCredentialDelivery}
	current,err:=e.credentialMaterial()
	if err!=nil||current!=m{return errCredentialDelivery}
	if ObserveNativeGatewayAttachmentsWithReader(ctx,e.plan.top,e.plan.devices,e.sdk)!=nil{return errCredentialDelivery}
	obs,err:=observePinnedGatewayInstances(ctx,e.plan.top,e.plan.pair,images,e.sdk)
	if err!=nil||len(obs)!=2||obs[0].Status!=NativeGatewayCompatible||obs[1].Status!=NativeGatewayCompatible{return errCredentialDelivery}
	for i,spec:=range []NativeGatewayInstanceSpec{e.plan.pair.Postgres,e.plan.pair.LiteLLM}{
		guest,_,err:=e.sdk.GetInstance(spec.Name)
		want:=r.PostgresCreatedAt;if i==1{want=r.LiteLLMCreatedAt}
		if err!=nil||guest==nil||!guest.CreatedAt.Equal(want)||guest.Stateful||!equalNativeDevices(guest.ExpandedDevices,spec.Devices){return errCredentialDelivery}
	}
	for _,entry:=range gatewayCredentialEntries(e.plan.pair,m){
		if !gatewayCredentialReadback(e.sdk,entry){return errCredentialDelivery}
	}
	return e.guard(ctx)
}
