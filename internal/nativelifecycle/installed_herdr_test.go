package nativelifecycle

import (
	"testing"
	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)
func TestHerdrEffectActionRequiresExactBootstrapAndTarget(t *testing.T){
	v:=orchestration.NativeEffect{Action:native.Create,Target:herdrclient.NativeVolumeID}
	if herdrEffectAction(herdrclient.NativeVolumeID,v,false)!=""||herdrEffectAction(herdrclient.NativeVolumeID,v,true)!="create-state"{t.Fatal("volume bootstrap authorization mismatch")}
	if herdrEffectAction(herdrclient.NativeInstanceID,v,true)!=""{t.Fatal("cross-target effect accepted")}
	i:=orchestration.NativeEffect{Action:native.Start,Target:herdrclient.NativeInstanceID}
	if herdrEffectAction(herdrclient.NativeInstanceID,i,false)!="start"{t.Fatal("Herdr start not selected")}
	i.Action=native.Delete
	if herdrEffectAction(herdrclient.NativeInstanceID,i,true)!=""{t.Fatal("reconcile admitted deletion")}
}
