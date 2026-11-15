package controllerauthority

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestHostEnrollmentCreateAndDurableRevokeWithoutAutomaticHandoff(t *testing.T) {
	dir:=t.TempDir()
	owner:=uint32(os.Geteuid())
	record:=testRecord()
	added,err:=createAt(context.Background(),dir,owner,record)
	if err!=nil||added.ControllerID!=record.ControllerID||added.Epoch!=1 {t.Fatalf("create: %+v %v",added,err)}
	if _,err:=createAt(context.Background(),dir,owner,record);err==nil{
		t.Fatal("duplicate enrollment accepted")
	}
	first,err:=loadOwnerFile(dir,owner)
	if err!=nil||len(first.entries)!=1 {t.Fatalf("readback: %+v %v",first,err)}
	ref:=EnrollmentReference{
		ControllerID:record.ControllerID,DeploymentID:record.DeploymentID,
		IncusProject:record.IncusProject,WorkloadID:record.WorkloadID,
	}
	if _,err:=revokeAt(context.Background(),dir,owner,ref,2);err==nil{
		t.Fatal("stale revocation epoch permitted")
	}
	revoked,err:=revokeAt(context.Background(),dir,owner,ref,1)
	if err!=nil||revoked.Status!="revoked"||revoked.Epoch!=2 {
		t.Fatalf("revoke: %+v %v",revoked,err)
	}
	if _,err:=revokeAt(context.Background(),dir,owner,ref,1);err==nil{
		t.Fatal("duplicate revoke silently succeeded")
	}
	if _,err:=createAt(context.Background(),dir,owner,record);err==nil{
		t.Fatal("re-enrolled an already revoked owner without handoff")
	}
	newOwner:=record
	newOwner.ControllerID="new-controller"
	newOwner.Domain="host"
	newOwner.UID=0
	newOwner.Certificate=""
	if _,err:=createAt(context.Background(),dir,owner,newOwner);err==nil{
		t.Fatal("adopted a revoked system/user resource")
	}
	reopened,err:=loadOwnerFile(dir,owner)
	if err!=nil||len(reopened.entries)!=1||
		reopened.entries[0].Status!="revoked"||reopened.entries[0].Epoch!=2 {
		t.Fatalf("lost tombstone: %+v %v",reopened,err)
	}
	for _,name:=range []string{hostEnrollmentFile,hostEnrollmentLock}{
		info,err:=os.Lstat(filepath.Join(dir,name))
		if err!=nil||!info.Mode().IsRegular()||info.Mode().Perm()!=0o600{
			t.Fatalf("nonprivate registry file %s: %+v %v",name,info,err)
		}
	}
}
func TestHostEnrollmentCreationFailsClosedOnCancellationAndUnsafeFilesystem(t *testing.T){
	dir:=t.TempDir()
	owner:=uint32(os.Geteuid())
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	if _,err:=createAt(ctx,dir,owner,testRecord());!errors.Is(err,context.Canceled){
		t.Fatalf("accepted canceled admin enrollment: %v",err)
	}
	if _,err:=os.Lstat(filepath.Join(dir,hostEnrollmentFile));!errors.Is(err,os.ErrNotExist){
		t.Fatal("canceled request wrote host manifest")
	}
	if err:=os.Chmod(dir,0o777);err!=nil{t.Fatal(err)}
	if _,err:=createAt(context.Background(),dir,owner,testRecord());err==nil{
		t.Fatal("accepted writable host directory")
	}
	if err:=os.Chmod(dir,0o700);err!=nil{t.Fatal(err)}
	foreign:=filepath.Join(t.TempDir(),"foreign.json")
	if err:=os.WriteFile(foreign,[]byte("sentinel"),0o600);err!=nil{t.Fatal(err)}
	if err:=os.Symlink(foreign,filepath.Join(dir,hostEnrollmentFile));err!=nil{t.Fatal(err)}
	if _,err:=createAt(context.Background(),dir,owner,testRecord());err==nil{
		t.Fatal("adopted foreign symlink as trusted manifest")
	}
	if content,err:=os.ReadFile(foreign);err!=nil||string(content)!="sentinel"{
		t.Fatalf("touched foreign state: %q %v",content,err)
	}
}
func TestHostEnrollmentRequiresExplicitRootForPublicOperations(t *testing.T){
	if os.Geteuid()==0 {t.Skip("test only applies to unprivileged CI users")}
	if _,err:=CreateHostControllerEnrollment(context.Background(),testRecord());err==nil{
		t.Fatal("nonroot issued host enrollment")
	}
	if _,err:=RevokeHostControllerEnrollment(context.Background(),EnrollmentReference{
		ControllerID:"x",DeploymentID:"d",IncusProject:"aginctus",WorkloadID:"w",
	},1);err==nil{t.Fatal("nonroot revoked host enrollment")}
}
