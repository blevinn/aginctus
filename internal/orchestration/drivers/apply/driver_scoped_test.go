package apply

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	incusapply "github.com/abiosoft/incus-apply/apply"
)

const scopedNativeInstanceConfig = `{
	"operation":"upsert",
	"project":"aginctus",
	"ensureRunning":true,
	"rejectUnsupportedChanges":true,
	"requireExistingConfig":{
		"user.aginctus.managed":"true",
		"user.aginctus.resource":"workload",
		"user.aginctus.role":"agent-workload",
		"user.aginctus.workload":"dev"
	},
	"documents":[{
		"kind":"instance","name":"aginctus-dev",
		"image":"aginctus-opencode-workload",
		"config":{"user.aginctus.managed":"true"}
	}]
}`

func TestPlanScopedInstanceUsesNativeReadOnlyProject(t *testing.T) {
	fake := &fakeApplyClient{}
	driver := New()
	calls := 0
	driver.newClient = func(opts incusapply.Options) applyClient {
		calls++
		if opts.Project != "aginctus" || opts.Remote != "" ||
			opts.Operation != incusapply.Upsert || !opts.EnsureRunning ||
			!opts.RejectUnsupportedChanges || !opts.FailFast {
			t.Fatalf("unsafe native options: %#v", opts)
		}
		if opts.RequireExistingConfig["user.aginctus.role"] != "agent-workload" {
			t.Fatalf("lost ownership guard: %#v", opts.RequireExistingConfig)
		}
		return fake
	}
	if _, err := driver.PlanScopedInstance(t.Context(), json.RawMessage(scopedNativeInstanceConfig)); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !fake.planned || fake.executed ||
		!strings.Contains(fake.input, "name: aginctus-dev") ||
		!strings.Contains(fake.input, "kind: instance") {
		t.Fatalf("native scoped plan: clients=%d planned=%t executed=%t input=%q",
			calls, fake.planned, fake.executed, fake.input)
	}
}

func TestPlanScopedInstanceRejectsUnsafeShapeBeforeClient(t *testing.T) {
	var base map[string]any
	if err := json.Unmarshal([]byte(scopedNativeInstanceConfig), &base); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(map[string]any){
		"default project":   func(m map[string]any) { m["project"] = "default" },
		"missing project":   func(m map[string]any) { delete(m, "project") },
		"wrong operation":   func(m map[string]any) { m["operation"] = "delete" },
		"missing operation": func(m map[string]any) { delete(m, "operation") },
		"no running policy": func(m map[string]any) { m["ensureRunning"] = false },
		"no drift guard":    func(m map[string]any) { m["rejectUnsupportedChanges"] = false },
		"no identity": func(m map[string]any) {
			delete(m["requireExistingConfig"].(map[string]any), "user.aginctus.workload")
		},
		"foreign role": func(m map[string]any) {
			m["requireExistingConfig"].(map[string]any)["user.aginctus.role"] = "herdr-client"
		},
		"two documents": func(m map[string]any) {
			m["documents"] = append(m["documents"].([]any),
				map[string]any{"kind": "network", "name": "aginctus-mgmt"})
		},
		"network resource": func(m map[string]any) {
			m["documents"] = []any{map[string]any{"kind": "network", "name": "aginctus-mgmt"}}
		},
		"remote prefix": func(m map[string]any) {
			m["documents"].([]any)[0].(map[string]any)["name"] = "remote:aginctus-dev"
		},
		"explicit remote": func(m map[string]any) {
			m["documents"].([]any)[0].(map[string]any)["remote"] = "remote"
		},
		"inline project": func(m map[string]any) {
			m["documents"].([]any)[0].(map[string]any)["project"] = "other"
		},
	}
	for name, modify := range cases {
		t.Run(name, func(t *testing.T) {
			var doc map[string]any
			if err := json.Unmarshal([]byte(scopedNativeInstanceConfig), &doc); err != nil {
				t.Fatal(err)
			}
			modify(doc)
			data, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			driver := New()
			driver.newClient = func(incusapply.Options) applyClient {
				t.Fatal("native client constructed for invalid scoped input")
				return nil
			}
			if _, err := driver.PlanScopedInstance(t.Context(), data); err == nil {
				t.Fatal("unsafe native scoped plan accepted")
			}
		})
	}
}

func TestPlanScopedInstanceHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	driver := New()
	driver.newClient = func(incusapply.Options) applyClient {
		t.Fatal("cancelled planner constructed a native client")
		return nil
	}
	if _, err := driver.PlanScopedInstance(ctx, json.RawMessage(scopedNativeInstanceConfig)); err == nil {
		t.Fatal("cancelled scoped planner succeeded")
	}
}
