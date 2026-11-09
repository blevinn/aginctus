package native

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"time"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration/nativeoci"
)

var nativeOCIDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}package native

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"time"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration/nativeoci"
)

)

// GatewayImageAcquireIntent binds one immutable public OCI manifest to the
// project selected by the caller. It is not an alias, deployment grant, or
// permission to adopt/relabel existing images.
type GatewayImageAcquireIntent struct {
	Project string
	Image nativeoci.ResolvedImage
}

func (in GatewayImageAcquireIntent) Validate() error {
	if _, err := incusscope.NewProjectScope(in.Project); err != nil {
		return fmt.Errorf("gateway OCI acquisition requires explicit nondefault project")
	}
	image := in.Image
	if image.Registry == "" || image.Repository == "" ||
		!nativeOCIDigest.MatchString(image.ManifestDigest) ||
		!nativeOCIDigest.MatchString(image.ConfigDigest) ||
		!fingerprintPattern.MatchString(image.IncusFingerprint) {
		return fmt.Errorf("gateway OCI acquisition requires complete immutable identity")
	}
	source := nativeoci.Source{
		Registry:image.Registry,Repository:image.Repository,Reference:image.ManifestDigest,
		// Platform is checked by nativeoci.ResolvePublic before this function.
	}
	// Source.Validate requires a platform; validate registry and repository by
	// supplying a fixed Linux platform solely for syntax verification.
	source.Platform.OS="linux"
	source.Platform.Architecture="amd64"
	if err := source.Validate(); err != nil {
		return fmt.Errorf("gateway OCI acquisition has invalid registry identity")
	}
	return nil
}

type GatewayImageAcquireResult struct {
	Project string `json:"project"`
	Fingerprint string `json:"fingerprint"`
	Attempted bool `json:"attempted"`
	Verified bool `json:"verified"`
}

type GatewayImageAcquireError struct {
	Stage string
	Attempted bool
	Uncertain bool
	cause error
}
func (e *GatewayImageAcquireError) Error() string {
	return "native gateway OCI acquisition failed at " + e.Stage + "; inspect image inventory before retry; no automatic cleanup"
}
func (e *GatewayImageAcquireError) Unwrap() error { return e.cause }

// GatewayImageAcquireClient cannot mutate aliases, delete images, or update
// existing daemon records. All image acquisition is daemon-side.
type GatewayImageAcquireClient interface {
	GetConnectionInfo() (*incus.ConnectionInfo,error)
	GetProject(string) (*api.Project,string,error)
	GetImage(string) (*api.Image,string,error)
	CreateImage(api.ImagesPost,*incus.ImageCreateArgs) (GatewayImageAcquireOperation,error)
}
type GatewayImageAcquireOperation interface {
	WaitContext(context.Context) error
	Get() *api.Operation
}
type sdkGatewayImageClient struct { server incus.InstanceServer }
func (c sdkGatewayImageClient) GetConnectionInfo() (*incus.ConnectionInfo,error) { return c.server.GetConnectionInfo() }
func (c sdkGatewayImageClient) GetProject(name string) (*api.Project,string,error) { return c.server.GetProject(name) }
func (c sdkGatewayImageClient) GetImage(name string) (*api.Image,string,error) { return c.server.GetImage(name) }
func (c sdkGatewayImageClient) CreateImage(req api.ImagesPost,args *incus.ImageCreateArgs) (GatewayImageAcquireOperation,error) {
	return c.server.CreateImage(req,args)
}

// AcquireLocalGatewayImage performs a tightly scoped create-only OCI pull.
// It rejects any preexisting matching Incus fingerprint, because Incus's
// layer hash is not evidence of OCI manifest/config equality.
func AcquireLocalGatewayImage(ctx context.Context, in GatewayImageAcquireIntent) (GatewayImageAcquireResult,error) {
	if err := in.Validate(); err != nil { return GatewayImageAcquireResult{},err }
	ctx,cancel:=context.WithTimeout(ctx,3*time.Minute)
	defer cancel()
	if err:=ctx.Err();err!=nil { return GatewayImageAcquireResult{},err }
	server,err:=incus.ConnectIncusUnixWithContext(ctx,"",&incus.ConnectionArgs{SkipGetServer:true})
	if err!=nil { return GatewayImageAcquireResult{},observationFailure(ctx,"local OCI image connection unavailable") }
	defer server.Disconnect()
	return AcquireGatewayImageWithClient(ctx,in,sdkGatewayImageClient{server:server.UseProject(in.Project)})
}

// AcquireGatewayImageWithClient is not idempotent when an image is present:
// safe reuse requires separately persisted manifest/config ownership evidence.
// Refusing a matching fingerprint is intentional and avoids layer-only adoption.
func AcquireGatewayImageWithClient(ctx context.Context,in GatewayImageAcquireIntent,client GatewayImageAcquireClient) (GatewayImageAcquireResult,error) {
	result:=GatewayImageAcquireResult{Project:in.Project,Fingerprint:in.Image.IncusFingerprint}
	fail:=func(stage string,attempted bool,cause error)(GatewayImageAcquireResult,error) {
		return result,&GatewayImageAcquireError{Stage:stage,Attempted:attempted,Uncertain:attempted,cause:cause}
	}
	if err:=in.Validate();err!=nil{return fail("intent",false,err)}
	if err:=ctx.Err();err!=nil{return fail("cancellation",false,err)}
	connection,err:=client.GetConnectionInfo()
	if err!=nil || connection==nil || connection.Protocol!="incus" ||
		!filepath.IsAbs(connection.SocketPath) || connection.Project!=in.Project {
		return fail("transport",false,fmt.Errorf("local selected-project transport unavailable"))
	}
	scope,_:=incusscope.NewProjectScope(in.Project)
	checkProject:=func()error {
		if err:=ctx.Err();err!=nil{return err}
		project,_,err:=client.GetProject(scope.Name)
		if err!=nil || project==nil || scope.AssessProject(project.Name,project.Config)!=incusscope.ProjectCompatible {
			return fmt.Errorf("selected project ownership or policy unverified")
		}
		return ctx.Err()
	}
	checkAbsent:=func()error {
		if err:=ctx.Err();err!=nil{return err}
		image,_,err:=client.GetImage(in.Image.IncusFingerprint)
		if api.StatusErrorCheck(err,http.StatusNotFound){return ctx.Err()}
		if err!=nil || image==nil {return fmt.Errorf("image absence unverified")}
		return fmt.Errorf("existing image fingerprint requires separate manifest/config identity verification")
	}
	if err:=checkProject();err!=nil{return fail("project",false,err)}
	if err:=checkAbsent();err!=nil{return fail("preflight",false,err)}
	if err:=checkProject();err!=nil{return fail("mutation-boundary",false,err)}
	if err:=checkAbsent();err!=nil{return fail("mutation-boundary",false,err)}
	if err:=ctx.Err();err!=nil{return fail("cancellation",false,err)}
	request:=api.ImagesPost{Source:&api.ImagesPostSource{
		Type:"image",Mode:"pull",ImageSource:api.ImageSource{
			Protocol:"oci",Server:"https://"+in.Image.Registry,
			Alias:in.Image.Repository+"@"+in.Image.ManifestDigest,ImageType:"container",
		},
	}}
	result.Attempted=true
	op,err:=client.CreateImage(request,nil)
	if err!=nil{return fail("create",true,err)}
	if op==nil{return fail("operation",true,fmt.Errorf("image operation unavailable"))}
	if err:=op.WaitContext(ctx);err!=nil{return fail("operation",true,err)}
	metadata:=op.Get()
	if metadata==nil || metadata.Metadata["fingerprint"]!=in.Image.IncusFingerprint {
		return fail("operation-identity",true,fmt.Errorf("daemon OCI layer fingerprint mismatch"))
	}
	if err:=checkProject();err!=nil{return fail("verification",true,err)}
	image,_,err:=client.GetImage(in.Image.IncusFingerprint)
	if err!=nil || image==nil || image.Fingerprint!=in.Image.IncusFingerprint ||
		image.Project!=in.Project || image.Type!="container" {
		return fail("verification",true,fmt.Errorf("imported image identity unverified"))
	}
	result.Verified=true
	return result,nil
}
