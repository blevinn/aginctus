package statevolume

import (
    "context"
    "fmt"
    "net/http"
    "regexp"

    incusclient "github.com/lxc/incus/v7/client"
    "github.com/lxc/incus/v7/shared/api"
)

// OAuthCredentials describes the dedicated Aginctus-managed gateway
// credential volume. This storage is deliberately NOT attached to LiteLLM,
// PostgreSQL, the Herdr client, or agent workloads.
type OAuthCredentials struct {
    Pool string
    GatewayID string
}

var gatewayID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

func (v OAuthCredentials) Validate() error {
    if !safeName.MatchString(v.Pool) || !gatewayID.MatchString(v.GatewayID) {
        return fmt.Errorf("unsafe OAuth credential volume pool or gateway ID")
    }
    return nil
}

func (v OAuthCredentials) Name() string {
    return "aginctus-gateway-" + v.GatewayID + "-oauth"
}

func (v OAuthCredentials) markers() map[string]string {
    return map[string]string{
        "user.aginctus.managed":"true",
        "user.aginctus.resource":"gateway-oauth-credentials",
        "user.aginctus.gateway":v.GatewayID,
    }
}

func VerifyOAuthCredentials(v OAuthCredentials, got *api.StorageVolume) error {
    if err:=v.Validate();err!=nil{return err}
    if got==nil || got.Name!=v.Name() || got.Type!="custom" || got.ContentType!="filesystem" {
        return fmt.Errorf("OAuth credential volume %q has unexpected name, type, or content",v.Name())
    }
    for key,expected:=range v.markers(){
        if got.Config[key]!=expected {
            return fmt.Errorf("OAuth credential volume %q ownership mismatch at %s",v.Name(),key)
        }
    }
    return nil
}

// EnsureOAuthCredentials creates only missing owned storage. Existing foreign
// names fail closed; retained volumes are never erased or adopted.
func EnsureOAuthCredentials(ctx context.Context, backend Backend, v OAuthCredentials) error {
    if err:=v.Validate();err!=nil{return err}
    got,err:=backend.Get(ctx,v.Pool,v.Name())
    if err==nil{return VerifyOAuthCredentials(v,got)}
    if _,ok:=err.(NotFound);!ok{return fmt.Errorf("inspect OAuth credential storage: %w",err)}
    if err:=ctx.Err();err!=nil{return err}
    req:=api.StorageVolumesPost{
        Name:v.Name(),Type:"custom",ContentType:"filesystem",
        StorageVolumePut:api.StorageVolumePut{
            Description:"Aginctus gateway OAuth credentials (no direct workload access)",
            Config:v.markers(),
        },
    }
    if err:=backend.Create(ctx,v.Pool,req);err!=nil{
        return fmt.Errorf("create OAuth credential volume %q: %w",v.Name(),err)
    }
    got,err=backend.Get(ctx,v.Pool,v.Name())
    if err!=nil{return fmt.Errorf("verify created OAuth credential volume: %w",err)}
    return VerifyOAuthCredentials(v,got)
}

// EnsureOAuthCredentialsOnHost uses the trusted local Incus socket. It does
// not mount or expose credentials, and is safe to repeat.
func EnsureOAuthCredentialsOnHost(ctx context.Context,v OAuthCredentials) error{
    if err:=v.Validate();err!=nil{return err}
    server,err:=incusclient.ConnectIncusUnixWithContext(ctx,"",&incusclient.ConnectionArgs{SkipGetServer:true})
    if err!=nil{return fmt.Errorf("connect local Incus for OAuth storage: %w",err)}
    server=server.UseProject(Project)
    // Do not permit a similarly named foreign project/resource to become
    // the destination for secrets once the gateway service is introduced.
    _,_,err=server.GetStoragePoolVolume(v.Pool,"custom",v.Name())
    if err!=nil && !api.StatusErrorCheck(err,http.StatusNotFound){
        return fmt.Errorf("inspect candidate credential storage: %w",err)
    }
    return EnsureOAuthCredentials(ctx,nativeBackend{server:server},v)
}
