package compose

import (
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
