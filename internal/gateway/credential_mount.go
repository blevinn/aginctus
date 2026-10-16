package gateway

import (
    "context"
    "fmt"
    "net/url"
    "strings"

    "github.com/blevinn/aginctus/internal/statevolume"
    incusclient "github.com/lxc/incus/v7/client"
    "github.com/lxc/incus/v7/shared/api"
)

const (
    credentialDevice = "aginctus_chatgpt_credentials"
    credentialMountPath = "/var/lib/aginctus/chatgpt"
    liteLLMServiceInstance = "litellm-1"
)

type CredentialAttachment struct {
    GatewayID string
    ComposeProject string
    Pool string
}

func (a CredentialAttachment) volume() statevolume.GatewayCredentials {
    return statevolume.GatewayCredentials{Pool: a.Pool, GatewayID: a.GatewayID, Project: a.ComposeProject}
}

func (a CredentialAttachment) Validate() error {
    if err:=a.volume().Validate();err!=nil{return err}
    if !gatewayIDPattern.MatchString(a.ComposeProject) {
        return fmt.Errorf("unsafe compose project name for credential attachment")
    }
    if a.ComposeProject=="default" {
        return fmt.Errorf("credential mount requires an isolated compose project")
    }
    return nil
}

func (a CredentialAttachment) device() map[string]string {
    return map[string]string{
        "type": "disk",
        "pool": a.Pool,
        "source": a.volume().Name(),
        "path": credentialMountPath,
    }
}

func ownedLiteLLM(instance *api.Instance, project string) error {
    if instance == nil || instance.Name != liteLLMServiceInstance ||
        instance.Config["user.aginctus.managed"] != "true" ||
        instance.Config["user.aginctus.resource"] != "compose-instance" ||
        instance.Config["user.aginctus.deployment"] != project {
        return fmt.Errorf("LiteLLM target is missing or not owned by gateway Compose deployment")
    }
    return nil
}

// volumeExclusivity forbids another instance or profile from sharing this
// credential-bearing filesystem volume, even if Incus permits multiple mounts.
func volumeExclusivity(v *api.StorageVolume, project string) error {
    if v==nil{return fmt.Errorf("credential volume is missing")}
    for _,usedBy:=range v.UsedBy {
        u,err:=url.Parse(usedBy)
        if err!=nil{return fmt.Errorf("invalid credential volume attachment reference")}
        if u.Path!="/1.0/instances/"+liteLLMServiceInstance ||
            u.Query().Get("project")!=project {
            return fmt.Errorf("credential volume has an attachment outside the authorized LiteLLM instance")
        }
    }
    return nil
}

func plannedCredentialDevice(instance *api.Instance, a CredentialAttachment) (bool,error) {
    if err:=ownedLiteLLM(instance,a.ComposeProject);err!=nil{return false,err}
    desired:=a.device()
    for name,existing:=range instance.ExpandedDevices {
        if name==credentialDevice {
            for key,value:=range desired {
                if existing[key]!=value { return false,fmt.Errorf("credential device has unexpected configuration") }
            }
            if len(existing)!=len(desired) {return false,fmt.Errorf("credential device includes unexpected settings")}
            return false,nil // already attached
        }
        // No other device may own or shadow the token directory.
        path:=existing["path"]
        if path==credentialMountPath || (path!="/" && path!="" &&
            (strings.HasPrefix(credentialMountPath,path+"/") || strings.HasPrefix(path,credentialMountPath+"/"))) {
            return false,fmt.Errorf("credential mount path overlaps existing device")
        }
    }
    return true,nil
}

// AttachCredentialVolume explicitly adds a private device to an existing
// owned LiteLLM instance. It never deploys the gateway or enables OAuth.
func AttachCredentialVolume(ctx context.Context,a CredentialAttachment) error {
    if err:=a.Validate();err!=nil{return err}
    client,err:=incusclient.ConnectIncusUnixWithContext(ctx,"",&incusclient.ConnectionArgs{SkipGetServer:true})
    if err!=nil{return fmt.Errorf("connect local Incus: %w",err)}
    project,_,err:=client.GetProject(a.ComposeProject)
    if err!=nil{return fmt.Errorf("inspect gateway Compose project: %w",err)}
    if project.Config["features.storage.volumes"]!="true" {
        return fmt.Errorf("gateway Compose project must have isolated storage volumes; refusing default-project credential volume")
    }
    server:=client.UseProject(a.ComposeProject)
    inst,etag,err:=server.GetInstance(liteLLMServiceInstance)
    if err!=nil{return fmt.Errorf("inspect owned LiteLLM instance: %w",err)}
    needed,err:=plannedCredentialDevice(inst,a)
    if err!=nil{return err}

    // Only touch the correct project's owned volume. No cross-project
    // adoption of the similarly named legacy default-project volume.
    vol:=a.volume()
    if err:=statevolume.EnsureGatewayCredentialsOnHost(ctx,vol);err!=nil{
        return fmt.Errorf("verify or provision private Compose-project volume: %w",err)
    }
    existing,_,err:=server.GetStoragePoolVolume(a.Pool,"custom",vol.Name())
    if err!=nil{return fmt.Errorf("inspect credential volume attachments: %w",err)}
    if err:=statevolume.VerifyGatewayCredentials(vol,existing);err!=nil{return err}
    if err:=volumeExclusivity(existing,a.ComposeProject);err!=nil{return err}
    if !needed{return nil}

    // Snapshot the writable instance config and preserve all unrelated devices.
    // ETag prevents overwriting concurrent updates.
    put:=inst.Writable()
    if put.Devices==nil{put.Devices=map[string]map[string]string{}}
    if _,collision:=put.Devices[credentialDevice];collision{
        return fmt.Errorf("credential device name already occupied")
    }
    put.Devices[credentialDevice]=a.device()
    op,err:=server.UpdateInstance(liteLLMServiceInstance,put,etag)
    if err!=nil{return fmt.Errorf("attach private credential volume: %w",err)}
    if err:=op.WaitContext(ctx);err!=nil{return fmt.Errorf("wait for credential volume attachment: %w",err)}
    return nil
}
