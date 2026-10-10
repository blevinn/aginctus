package compose

import (
	"reflect"
	"strings"
	"testing"
)

func TestPlanComposeRecreationPreservesExplicitVolumes(t *testing.T) {
	drift := &ComposeDriftError{Kind: ComposeDriftImage, Reason: "image changed"}
	plan, err := planComposeRecreation(drift, map[string]map[string]string{
		"root":{"type":"disk","path":"/","pool":"default"},
		"data":{"type":"disk","path":"/var/lib/postgresql/data","pool":"default","source":"pg-data"},
		"cache":{"type":"disk","path":"/cache","pool":"default","source":"cache-data"},
		"eth0":{"type":"nic","network":"default"},
	})
	if err != nil { t.Fatal(err) }
	if !plan.Required || !reflect.DeepEqual(plan.PersistentDevices, []string{"cache","data"}) {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanComposeRecreationIsNoopWithoutDrift(t *testing.T) {
	plan, err := planComposeRecreation(nil, map[string]map[string]string{"data":{"type":"disk"}})
	if err != nil || plan.Required || len(plan.PersistentDevices)!=0 {
		t.Fatalf("plan=%#v error=%v", plan, err)
	}
}

func TestPlanComposeRecreationRejectsAmbiguousStorage(t *testing.T) {
	drift := &ComposeDriftError{Kind: ComposeDriftDevice}
	cases := map[string]map[string]string{
		"missing mount":{"type":"disk","pool":"default","source":"volume"},
		"bind mount":{"type":"disk","path":"/data","source":"/host/data"},
		"unspecified source":{"type":"disk","path":"/data","pool":"default"},
		"invalid reference":{"type":"disk","path":"/data","pool":" default","source":"volume"},
	}
	for name, device := range cases {
		t.Run(name,func(t *testing.T){
			_, err := planComposeRecreation(drift, map[string]map[string]string{"disk":device})
			if err==nil || strings.Contains(err.Error(),"/host/data") {
				t.Fatalf("expected secret-safe refusal, got %v",err)
			}
		})
	}
}
