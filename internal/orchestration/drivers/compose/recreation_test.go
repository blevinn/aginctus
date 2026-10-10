package compose

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestPlanComposeRecreationPreservesExplicitVolumes(t *testing.T) {
	drift := &ComposeDriftError{Kind: ComposeDriftImage, Reason: "image changed"}
	plan, err := planComposeRecreation(drift, map[string]map[string]string{
		"root":  {"type": "disk", "path": "/", "pool": "default"},
		"data":  {"type": "disk", "path": "/var/lib/postgresql/data", "pool": "default", "source": "pg-data"},
		"cache": {"type": "disk", "path": "/cache", "pool": "default", "source": "cache-data"},
		"eth0":  {"type": "nic", "network": "default"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Required || !reflect.DeepEqual(plan.PersistentDevices, []string{"cache", "data"}) {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanComposeRecreationIsNoopWithoutDrift(t *testing.T) {
	plan, err := planComposeRecreation(nil, map[string]map[string]string{"data": {"type": "disk"}})
	if err != nil || plan.Required || len(plan.PersistentDevices) != 0 {
		t.Fatalf("plan=%#v error=%v", plan, err)
	}
}

func TestPlanComposeRecreationRejectsAmbiguousStorage(t *testing.T) {
	drift := &ComposeDriftError{Kind: ComposeDriftDevice}
	cases := map[string]map[string]string{
		"missing mount":      {"type": "disk", "pool": "default", "source": "volume"},
		"bind mount":         {"type": "disk", "path": "/data", "source": "/host/data"},
		"unspecified source": {"type": "disk", "path": "/data", "pool": "default"},
		"invalid reference":  {"type": "disk", "path": "/data", "pool": " default", "source": "volume"},
	}
	for name, device := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := planComposeRecreation(drift, map[string]map[string]string{"disk": device})
			if err == nil || strings.Contains(err.Error(), "/host/data") {
				t.Fatalf("expected secret-safe refusal, got %v", err)
			}
		})
	}
}

func TestRecreateWithoutPersistentDisksSequence(t *testing.T) {
	var calls []string
	call := func(s string) func(context.Context) error {
		return func(context.Context) error { calls=append(calls,s); return nil }
	}
	err:=recreateWithoutPersistentDisks(context.Background(),map[string]map[string]string{
		"root":{"type":"disk","path":"/","pool":"default"},
		"eth0":{"type":"nic"},
	},call("verify"),call("stop"),call("delete"),call("create"))
	if err!=nil {t.Fatal(err)}
	if !reflect.DeepEqual(calls,[]string{"verify","stop","verify","delete","create"}) {
		t.Fatalf("call order = %v",calls)
	}
}
func TestRecreateWithoutPersistentDisksRejectsVolumesBeforeMutation(t *testing.T) {
	called:=false
	action:=func(context.Context) error {called=true;return nil}
	err:=recreateWithoutPersistentDisks(context.Background(),map[string]map[string]string{
		"data":{"type":"disk","path":"/data","pool":"default","source":"data"},
	},action,action,action,action)
	if err==nil || called {t.Fatalf("error=%v mutation=%t",err,called)}
}
func TestRecreateWithoutPersistentDisksFailsClosed(t *testing.T) {
	for _,tc:=range []struct{name,stage string}{
		{"ownership before stop","initial"},{"ownership before delete","second"},
		{"stop failure","stop"},{"delete failure","delete"},{"create failure","create"},
	}{
		t.Run(tc.name,func(t *testing.T){
			want:=errors.New("failed")
			verifyCount:=0
			action:=func(stage string) func(context.Context) error {
				return func(context.Context)error {
					if stage=="verify" {verifyCount++; if tc.stage=="initial" && verifyCount==1 || tc.stage=="second" && verifyCount==2{return want}}
					if stage==tc.stage{return want}
					return nil
				}
			}
			err:=recreateWithoutPersistentDisks(context.Background(),nil,action("verify"),action("stop"),action("delete"),action("create"))
			if !errors.Is(err,want){t.Fatalf("error=%v, want original cause",err)}
		})
	}
}
