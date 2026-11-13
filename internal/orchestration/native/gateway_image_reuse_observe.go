package native

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// GatewayImageReuseReader exposes read-only, project-bound Incus observations.
// The assessed evidence must originate in a separately protected accepted
// record; observing a matching alias is never sufficient deployment authority.
type GatewayImageReuseReader interface {
	GetConnectionInfo() (*incus.ConnectionInfo,error)
	GetProject(string) (*api.Project,string,error)
	GetImageAlias(string) (*api.ImageAliasesEntry,string,error)
	GetImage(string) (*api.Image,string,error)
}

type sdkGatewayImageReuseReader struct{ server incus.InstanceServer }
func (r sdkGatewayImageReuseReader) GetConnectionInfo() (*incus.ConnectionInfo,error) { return r.server.GetConnectionInfo() }
func (r sdkGatewayImageReuseReader) GetProject(name string) (*api.Project,string,error) { return r.server.GetProject(name) }
func (r sdkGatewayImageReuseReader) GetImageAlias(name string) (*api.ImageAliasesEntry,string,error) { return r.server.GetImageAlias(name) }
func (r sdkGatewayImageReuseReader) GetImage(name string) (*api.Image,string,error) { return r.server.GetImage(name) }

// ObserveLocalGatewayImageReuse checks the selected project's existing image
// identity. It does not create or update aliases/images and cannot authorize a
// future mutation without independent accepted-state and writer-fencing gates.
func ObserveLocalGatewayImageReuse(ctx context.Context,want GatewayImageAcquireIntent,aliasName string,evidence GatewayImageEvidence) error {
	if err:=want.Validate();err!=nil {return err}
	ctx,cancel:=context.WithTimeout(ctx,30*time.Second)
	defer cancel()
	if err:=ctx.Err();err!=nil {return err}
	server,err:=incus.ConnectIncusUnixWithContext(ctx,"",&incus.ConnectionArgs{SkipGetServer:true})
	if err!=nil {return observationFailure(ctx,"local gateway image observation unavailable")}
	defer server.Disconnect()
	return ObserveGatewayImageReuseWithClient(ctx,want,aliasName,evidence,sdkGatewayImageReuseReader{server:server.UseProject(want.Project)})
}

// ObserveGatewayImageReuseWithClient reads the explicit project, alias and
// image. Any unknown, inaccessible, foreign, or mismatched observation fails
// closed. This API has no write methods.
func ObserveGatewayImageReuseWithClient(ctx context.Context,want GatewayImageAcquireIntent,aliasName string,evidence GatewayImageEvidence,client GatewayImageReuseReader) error {
	if err:=ctx.Err();err!=nil {return err}
	if err:=want.Validate();err!=nil {return fmt.Errorf("gateway image reuse observation: invalid intent")}
	if !localName(aliasName) || evidence.Project!=want.Project || evidence.Alias!=aliasName || evidence.Identity!=want.Image {
		return fmt.Errorf("gateway image reuse observation: accepted identity evidence mismatch")
	}
	info,err:=client.GetConnectionInfo()
	if err!=nil || info==nil || info.Protocol!="incus" || !filepath.IsAbs(info.SocketPath) || info.Project!=want.Project {
		return fmt.Errorf("gateway image reuse observation: selected local transport unverified")
	}
	scope,_:=incusscope.NewProjectScope(want.Project)
	project,_,err:=client.GetProject(want.Project)
	if err!=nil || project==nil || scope.AssessProject(project.Name,project.Config)!=incusscope.ProjectCompatible {
		return fmt.Errorf("gateway image reuse observation: selected project policy unverified")
	}
	if err:=ctx.Err();err!=nil {return err}
	alias,_,err:=client.GetImageAlias(aliasName)
	if err!=nil || alias==nil || alias.Name!=aliasName || alias.Target!=want.Image.IncusFingerprint {
		return fmt.Errorf("gateway image reuse observation: alias unverified")
	}
	if err:=ctx.Err();err!=nil {return err}
	image,_,err:=client.GetImage(alias.Target)
	if err!=nil || image==nil {
		return fmt.Errorf("gateway image reuse observation: image unverified")
	}
	if err:=AssessGatewayImageReuse(want,aliasName,evidence,alias,image);err!=nil {return err}
	return ctx.Err()
}
