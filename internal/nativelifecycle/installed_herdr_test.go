package nativelifecycle

import (
	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	"testing"
)

func TestHerdrEffectActionRequiresExactBootstrapAndTarget(t *testing.T) {
	v := orchestration.NativeEffect{Action: native.Create, Target: herdrclient.NativeVolumeID}
	if herdrEffectAction(herdrclient.NativeVolumeID, v, false) != "" || herdrEffectAction(herdrclient.NativeVolumeID, v, true) != "create-state" {
		t.Fatal("volume bootstrap authorization mismatch")
	}
	if herdrEffectAction(herdrclient.NativeInstanceID, v, true) != "" {
		t.Fatal("cross-target effect accepted")
	}
	i := orchestration.NativeEffect{Action: native.Start, Target: herdrclient.NativeInstanceID}
	if herdrEffectAction(herdrclient.NativeInstanceID, i, false) != "start" {
		t.Fatal("Herdr start not selected")
	}
	i.Action = native.Delete
	if herdrEffectAction(herdrclient.NativeInstanceID, i, true) != "" {
		t.Fatal("reconcile admitted deletion")
	}
}

func TestInstalledHerdrTeardownEffects(t *testing.T) {
	for _,tc:=range []struct{op native.ProjectInstanceOperation;action native.Action;target string;want string}{
		{native.StopInstance,native.Stop,herdrclient.NativeInstanceID,"stop"},
		{native.RemoveInstance,native.Delete,herdrclient.NativeInstanceID,"delete"},
		{native.StopInstance,native.Delete,herdrclient.NativeInstanceID,""},
		{native.RemoveInstance,native.Stop,herdrclient.NativeInstanceID,""},
		{native.RemoveInstance,native.Delete,herdrclient.NativeVolumeID,""},
	}{
		e:=orchestration.NativeEffect{Action:tc.action,Target:tc.target}
		if got:=installedHerdrEffectAction(tc.target,e,false,tc.op);got!=tc.want{t.Fatalf("unexpected teardown action %q",got)}
	}
}
