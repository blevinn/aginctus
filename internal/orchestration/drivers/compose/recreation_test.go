package compose

import (
	"context"
	composeclient "github.com/lxc/incus-compose/client"
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
		return func(context.Context) error { calls = append(calls, s); return nil }
	}
	err := recreateWithoutPersistentDisks(context.Background(), map[string]map[string]string{
		"root": {"type": "disk", "path": "/", "pool": "default"},
		"eth0": {"type": "nic"},
	}, call("verify"), call("stop"), call("delete"), call("create"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"verify", "stop", "verify", "delete", "create"}) {
		t.Fatalf("call order = %v", calls)
	}
}
func TestRecreateWithoutPersistentDisksRejectsVolumesBeforeMutation(t *testing.T) {
	called := false
	action := func(context.Context) error { called = true; return nil }
	err := recreateWithoutPersistentDisks(context.Background(), map[string]map[string]string{
		"data": {"type": "disk", "path": "/data", "pool": "default", "source": "data"},
	}, action, action, action, action)
	if err == nil || called {
		t.Fatalf("error=%v mutation=%t", err, called)
	}
}
func TestRecreateWithoutPersistentDisksFailsClosed(t *testing.T) {
	for _, tc := range []struct{ name, stage string }{
		{"ownership before stop", "initial"}, {"ownership before delete", "second"},
		{"stop failure", "stop"}, {"delete failure", "delete"}, {"create failure", "create"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := errors.New("failed")
			verifyCount := 0
			action := func(stage string) func(context.Context) error {
				return func(context.Context) error {
					if stage == "verify" {
						verifyCount++
						if tc.stage == "initial" && verifyCount == 1 || tc.stage == "second" && verifyCount == 2 {
							return want
						}
					}
					if stage == tc.stage {
						return want
					}
					return nil
				}
			}
			err := recreateWithoutPersistentDisks(context.Background(), nil, action("verify"), action("stop"), action("delete"), action("create"))
			if !errors.Is(err, want) {
				t.Fatalf("error=%v, want original cause", err)
			}
		})
	}
}

func TestVerifyComposeRecreationVolumes(t *testing.T) {
	existing := map[string]map[string]string{
		"root": {"type": "disk", "path": "/", "pool": "default"},
		"data": {"type": "disk", "path": "/var/lib/postgresql/data", "pool": "default", "source": "postgres-data"},
		"eth0": {"type": "nic", "network": "default"},
	}
	cases := []struct {
		name        string
		replacement map[string]map[string]string
		wantError   bool
	}{
		{"preserves volume", map[string]map[string]string{"data": {"type": "disk", "path": "/var/lib/postgresql/data", "pool": "default", "source": "postgres-data"}}, false},
		{"removes volume", map[string]map[string]string{}, true},
		{"changes source", map[string]map[string]string{"data": {"type": "disk", "path": "/var/lib/postgresql/data", "pool": "default", "source": "different"}}, true},
		{"changes pool", map[string]map[string]string{"data": {"type": "disk", "path": "/var/lib/postgresql/data", "pool": "other", "source": "postgres-data"}}, true},
		{"changes mount path", map[string]map[string]string{"data": {"type": "disk", "path": "/data", "pool": "default", "source": "postgres-data"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyComposeRecreationVolumes(existing, tc.replacement)
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v wantError=%t", err, tc.wantError)
			}
			if err != nil && strings.Contains(err.Error(), "postgres-data") {
				t.Fatalf("diagnostic leaked volume source: %v", err)
			}
		})
	}
}

func TestVerifyComposeRecreationVolumesRejectsAmbiguousExistingDisk(t *testing.T) {
	err := verifyComposeRecreationVolumes(
		map[string]map[string]string{"bind": {"type": "disk", "path": "/data", "source": "/host/data"}},
		map[string]map[string]string{"bind": {"type": "disk", "path": "/data", "source": "/host/data"}},
	)
	if err == nil {
		t.Fatal("expected failure for non-volume disk")
	}
}

func TestRetainedVolumeIdentityRejectsChangedOrMissingDisks(t *testing.T) {
	old:=map[string]map[string]string{"data":{"type":"disk","path":"/data","pool":"default","source":"pg-data"}}
	good:=map[string]map[string]string{"data":{"type":"disk","path":"/data","pool":"default","source":"pg-data"}}
	if err:=verifyComposeRecreationVolumes(old,good);err!=nil{t.Fatal(err)}
	for name, replacement:=range map[string]map[string]map[string]string{
		"removed":{},
		"rebound":{"data":{"type":"disk","path":"/data","pool":"default","source":"other"}},
		"moved":{"data":{"type":"disk","path":"/elsewhere","pool":"default","source":"pg-data"}},
	} {
		t.Run(name,func(t *testing.T) {
			if err:=verifyComposeRecreationVolumes(old,replacement);err==nil{t.Fatal("unsafe recreation permitted")}
		})
	}
}
func TestRetainedVolumeRecreationChecksBeforeMutations(t *testing.T) {
	called:=false
	fn:=func(context.Context)error{called=true;return nil}
	err:=recreateWithRetainedVolumes(context.Background(),
		map[string]map[string]string{"data":{"type":"disk","path":"/data","pool":"default","source":"old"}},
		map[string]map[string]string{"data":{"type":"disk","path":"/data","pool":"default","source":"new"}},
		fn,fn,fn,fn)
	if err==nil || called {t.Fatalf("error=%v called=%t",err,called)}
}
func TestMaterializeComposeRawDevices(t *testing.T) {
	config:=composeclient.InstanceConfig{ExtraDevices:map[string]map[string]string{
		"data":{"type":"disk","pool":"default","path":"/data","source":"pg-data"},
	}}
	m,err:=materializeComposeDevices(config)
	if err!=nil || m["data"]["source"]!="pg-data" {t.Fatalf("devices=%v error=%v",m,err)}
}
