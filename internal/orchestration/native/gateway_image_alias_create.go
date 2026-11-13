package native

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// GatewayAliasCreateIntent refers only to a freshly acquired, verified image.
// The acquisition result must be trusted within the caller's fenced operation:
// constructing this struct is not an authorization mechanism.
type GatewayAliasCreateIntent struct {
	Project string
	Alias string
	Image GatewayOCIIdentity
	Acquisition GatewayImageAcquireResult
}

func (in GatewayAliasCreateIntent) Validate() error {
	if !localName(in.Alias) {
		return fmt.Errorf("gateway alias must be an explicit safe name")
	}
	if err:=(GatewayImageAcquireIntent{Project:in.Project,Image:in.Image}).Validate();err!=nil {
		return fmt.Errorf("gateway alias requires complete immutable image identity")
	}
	if !in.Acquisition.Attempted || !in.Acquisition.Verified ||
		in.Acquisition.Project!=in.Project || in.Acquisition.Fingerprint!=in.Image.IncusFingerprint {
		return fmt.Errorf("gateway alias requires a matching verified acquisition result")
	}
	return nil
}

type GatewayAliasCreateResult struct {
	Project string `json:"project"`
	Alias string `json:"alias"`
	Created bool `json:"created"`
	Verified bool `json:"verified"`
}
type GatewayAliasCreateError struct {
	Stage string
	Attempted bool
	Uncertain bool
	cause error
}
func (e *GatewayAliasCreateError) Error() string {
	return "native gateway image alias creation failed at "+e.Stage+"; inspect exact alias and image state before retry"
}
func (e *GatewayAliasCreateError) Unwrap() error { return e.cause }

// GatewayAliasCreateClient permits only alias creation, not alias rewrites,
// image deletion, adoption, or cross-project image copying.
type GatewayAliasCreateClient interface {
	GetConnectionInfo() (*incus.ConnectionInfo,error)
	GetProject(string) (*api.Project,string,error)
	GetImage(string) (*api.Image,string,error)
	GetImageAlias(string) (*api.ImageAliasesEntry,string,error)
	CreateImageAlias(api.ImageAliasesPost) error
}
type sdkGatewayAliasCreateClient struct{ server incus.InstanceServer }
func (c sdkGatewayAliasCreateClient) GetConnectionInfo() (*incus.ConnectionInfo,error){return c.server.GetConnectionInfo()}
func (c sdkGatewayAliasCreateClient) GetProject(s string)(*api.Project,string,error){return c.server.GetProject(s)}
func (c sdkGatewayAliasCreateClient) GetImage(s string)(*api.Image,string,error){return c.server.GetImage(s)}
func (c sdkGatewayAliasCreateClient) GetImageAlias(s string)(*api.ImageAliasesEntry,string,error){return c.server.GetImageAlias(s)}
func (c sdkGatewayAliasCreateClient) CreateImageAlias(r api.ImageAliasesPost)error{return c.server.CreateImageAlias(r)}

// CreateLocalGatewayImageAlias is a create-only, explicitly project-scoped
// primitive. A verified prior acquisition and daemon-wide writer lease are
// mandatory caller responsibilities, as is accepted-state authorization.
func CreateLocalGatewayImageAlias(ctx context.Context,in GatewayAliasCreateIntent)(GatewayAliasCreateResult,error){
	if err:=in.Validate();err!=nil{return GatewayAliasCreateResult{},err}
	ctx,cancel:=context.WithTimeout(ctx,30*time.Second)
	defer cancel()
	if err:=ctx.Err();err!=nil{return GatewayAliasCreateResult{},err}
	server,err:=incus.ConnectIncusUnixWithContext(ctx,"",&incus.ConnectionArgs{SkipGetServer:true})
	if err!=nil{return GatewayAliasCreateResult{},observationFailure(ctx,"local gateway alias connection unavailable")}
	defer server.Disconnect()
	return CreateGatewayImageAliasWithClient(ctx,in,sdkGatewayAliasCreateClient{server:server.UseProject(in.Project)})
}

// CreateGatewayImageAliasWithClient refuses any preexisting alias, including
// an apparently compatible one. Reuse is a separate accepted-evidence path.
func CreateGatewayImageAliasWithClient(ctx context.Context,in GatewayAliasCreateIntent,client GatewayAliasCreateClient)(GatewayAliasCreateResult,error){
	result:=GatewayAliasCreateResult{Project:in.Project,Alias:in.Alias}
	fail:=func(stage string,attempted bool,cause error)(GatewayAliasCreateResult,error){
		return result,&GatewayAliasCreateError{Stage:stage,Attempted:attempted,Uncertain:attempted,cause:cause}
	}
	if err:=in.Validate();err!=nil{return fail("intent",false,err)}
	if err:=ctx.Err();err!=nil{return fail("cancellation",false,err)}
	info,err:=client.GetConnectionInfo()
	if err!=nil || info==nil || info.Protocol!="incus" || !filepath.IsAbs(info.SocketPath) || info.Project!=in.Project {
		return fail("transport",false,fmt.Errorf("local selected-project transport unavailable"))
	}
	scope,_:=incusscope.NewProjectScope(in.Project)
	preflight:=func()error{
		if err:=ctx.Err();err!=nil{return err}
		project,_,err:=client.GetProject(in.Project)
		if err!=nil || project==nil || scope.AssessProject(project.Name,project.Config)!=incusscope.ProjectCompatible {
			return fmt.Errorf("selected project policy unverified")
		}
		image,_,err:=client.GetImage(in.Image.IncusFingerprint)
		if err!=nil || image==nil || image.Project!=in.Project || image.Type!="container" ||
			image.Fingerprint!=in.Image.IncusFingerprint {
			return fmt.Errorf("freshly acquired image identity unverified")
		}
		alias,_,err:=client.GetImageAlias(in.Alias)
		if api.StatusErrorCheck(err,http.StatusNotFound){return ctx.Err()}
		if err!=nil || alias==nil{return fmt.Errorf("alias absence unverified")}
		return fmt.Errorf("image alias already exists; create-only operation refuses adoption")
	}
	if err:=preflight();err!=nil{return fail("preflight",false,err)}
	if err:=preflight();err!=nil{return fail("mutation-boundary",false,err)}
	if err:=ctx.Err();err!=nil{return fail("cancellation",false,err)}
	req:=api.ImageAliasesPost{ImageAliasesEntry:api.ImageAliasesEntry{
		Name:in.Alias,ImageAliasesEntryPut:api.ImageAliasesEntryPut{Target:in.Image.IncusFingerprint},
	}}
	if err:=client.CreateImageAlias(req);err!=nil{return fail("create",true,err)}
	result.Created=true
	// The postcondition is intentionally separate: after an attempted create,
	// lookup failures and collisions are uncertain, never automatic cleanup.
	project,_,err:=client.GetProject(in.Project)
	if err!=nil || project==nil || scope.AssessProject(project.Name,project.Config)!=incusscope.ProjectCompatible{
		return fail("verification",true,fmt.Errorf("selected project policy unverified"))
	}
	alias,_,err:=client.GetImageAlias(in.Alias)
	if err!=nil || alias==nil || alias.Name!=in.Alias || alias.Target!=in.Image.IncusFingerprint {
		return fail("verification",true,fmt.Errorf("created alias identity unverified"))
	}
	image,_,err:=client.GetImage(in.Image.IncusFingerprint)
	if err!=nil || image==nil || image.Fingerprint!=in.Image.IncusFingerprint ||
		image.Project!=in.Project || image.Type!="container" {
		return fail("verification",true,fmt.Errorf("created alias target image unverified"))
	}
	if err:=ctx.Err();err!=nil{return fail("verification",true,err)}
	result.Verified=true
	return result,nil
}
