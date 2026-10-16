package gateway

import (
 "strings"
 "testing"

 "github.com/lxc/incus/v7/shared/api"
)

func attachmentFixture() CredentialAttachment {
 return CredentialAttachment{GatewayID:"local",ComposeProject:"aginctus-gateway",Pool:"default"}
}
func ownedInstance() *api.Instance {
 return &api.Instance{
  Name:liteLLMServiceInstance,
  InstancePut:api.InstancePut{
   Config:map[string]string{
    "user.aginctus.managed":"true",
    "user.aginctus.resource":"compose-instance",
    "user.aginctus.deployment":"aginctus-gateway",
   },
   Devices:map[string]map[string]string{},
  },
  ExpandedDevices:map[string]map[string]string{
   "root":{"type":"disk","path":"/","pool":"default"},
  },
 }
}
func TestPrivateMountPlacement(t *testing.T) {
 a:=attachmentFixture()
 if err:=a.Validate();err!=nil{t.Fatal(err)}
 if a.volume().Name()!="aginctus-gateway-local-credentials"||a.volume().Project!="aginctus-gateway"{t.Fatal(a)}
 device:=a.device()
 if device["type"]!="disk"||device["pool"]!="default"||device["path"]!="/var/lib/aginctus/chatgpt"{
  t.Fatal(device)
 }
 inst:=ownedInstance()
 needed,err:=plannedCredentialDevice(inst,a);if err!=nil||!needed{t.Fatalf("%v %v",needed,err)}
 inst.ExpandedDevices[credentialDevice]=device
 needed,err=plannedCredentialDevice(inst,a);if err!=nil||needed{t.Fatalf("expected idempotence, needed=%v err=%v",needed,err)}
}
func TestAttachmentFailsClosedOnOwnershipAndCollisions(t *testing.T){
 a:=attachmentFixture()
 inst:=ownedInstance()
 inst.Config["user.aginctus.resource"]="workload"
 if _,err:=plannedCredentialDevice(inst,a);err==nil{t.Fatal("accepted unowned instance")}
 inst=ownedInstance()
 inst.ExpandedDevices["foreign"]=map[string]string{"type":"disk","path":"/var/lib/aginctus","source":"wrong"}
 if _,err:=plannedCredentialDevice(inst,a);err==nil{t.Fatal("accepted overlapping mount")}
 inst=ownedInstance()
 inst.ExpandedDevices[credentialDevice]=map[string]string{"type":"disk","path":credentialMountPath,"source":"foreign"}
 if _,err:=plannedCredentialDevice(inst,a);err==nil{t.Fatal("accepted modified credential disk")}
 inst=ownedInstance()
 inst.ExpandedDevices["duplicate"]=map[string]string{"type":"disk","path":"/other","pool":a.Pool,"source":a.volume().Name()}
 if _,err:=plannedCredentialDevice(inst,a);err==nil{t.Fatal("accepted duplicate private volume mount")}
}
func TestAttachmentRejectsForeignConsumers(t *testing.T){
 good:="/1.0/instances/litellm-1?project=aginctus-gateway"
 for _,tc:=range []struct{used []string; valid bool}{
  {nil,true},
  {[]string{good},true},
  {[]string{good,"/1.0/instances/postgres-1?project=aginctus-gateway"},false},
  {[]string{"/1.0/instances/litellm-1?project=default"},false},
  {[]string{"/1.0/profiles/default?project=aginctus-gateway"},false},
 } {
  err:=volumeExclusivity(&api.StorageVolume{StorageVolumePut:api.StorageVolumePut{},UsedBy:tc.used},"aginctus-gateway")
  if (err==nil)!=tc.valid{t.Fatalf("used=%v err=%v",tc.used,err)}
 }
}
func TestAttachmentRejectsUnsafeProject(t *testing.T){
 a:=attachmentFixture()
 for _,project:=range []string{"default","../escape","", "a/b"}{
  a.ComposeProject=project
  if err:=a.Validate();err==nil{t.Fatalf("accepted project %q",project)}
 }
 a=attachmentFixture()
 if strings.Contains(a.device()["source"],"oauth"){t.Fatal("OAuth-specific mount")}
}
