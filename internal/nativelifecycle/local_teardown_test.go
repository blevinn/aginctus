package nativelifecycle

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
	check "github.com/blevinn/aginctus/pkg/orchestration"
)

func TestPinnedLocalTeardownRefusesUnapprovedOrDriftedEffects(t *testing.T) {
	for _, scenario := range []string{"missing-policy", "orphaned-state", "excess-pin", "running", "instance-pin", "volume-pin", "revoked-after-journal", "replaced-after-grant", "accepted"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := t.Context()
			f := newFixture(t)
			if _, err := f.execute(t, false, func(orchestration.NativeEffect) error { return nil }); err != nil {
				t.Fatal(err)
			}
			f.project.submissions = nil
			f.accepted.Workloads[0].Native = &acceptedstate.WorkloadNativeSpec{Runtime: "opencode", StoragePool: "approved-pool", ManagementNetwork: "approved-bridge"}
			f.accepted.Herdr = &environment.HerdrSpec{InstanceName: "herdr-1", ImageFingerprint: f.project.fingerprint, StoragePool: "approved-pool", ManagementNetwork: "approved-bridge"}
			enrolled := f.accepted.Environment
			store, err := environment.InitializeLocalProjectStateStore(filepath.Join(t.TempDir(), "project"), enrolled.Binding)
			if err != nil {
				t.Fatal(err)
			}
			rev, err := store.CommitLocalCandidate(ctx, enrolled, enrolled.Binding, nil, f.accepted)
			if err != nil {
				t.Fatal(err)
			}
			plan := lifecyclePlan(rev, "agent")
			plan.Resources = plan.Resources[:3]
			plan.Resources[1].DependsOn, plan.Resources[2].DependsOn = nil, nil
			pins := make(map[string]time.Time, 2)
			for id, name := range map[string]string{"agent": "agent-1", herdrclient.NativeInstanceID: "herdr-1"} {
				i := f.project.instances[name]
				pins[id] = i.CreatedAt
				if scenario != "running" {
					i.Status = "Stopped"
					f.project.instances[name] = i
				}
			}
			selection := orchestration.LocalLifecycleTeardown{Operation: native.RemoveInstance, Instances: pins, HerdrState: f.project.volume.CreatedAt}
			composer := LocalComposer{Environment: enrolled, TrustedServerPin: f.pin, Client: f.project, VerifyAttachments: f.verify}
			submits := 0
			exec := orchestration.LocalNativeExecution{Store: store, Environment: enrolled, BindLifecycleTeardown: composer.BindTeardown,
				Authorize: func(context.Context, orchestration.LocalNativeAuthorityRequest) (environment.Binding, error) {
					return enrolled.Binding, nil
				},
				AuthorizeTeardown: func(_ context.Context, r orchestration.LocalLifecycleTeardownRequest) (environment.Binding, error) {
					if r.Plan.Revision != rev || r.Selection.Operation != native.RemoveInstance || !r.Selection.Instances["agent"].Equal(pins["agent"]) || !r.Selection.HerdrState.Equal(selection.HerdrState) {
						t.Fatal("dedicated policy received the wrong selection")
					}
					// Authority gets a detached map; its mutation cannot retarget execution.
					delete(r.Selection.Instances, "agent")
					if r.Permission == orchestration.LocalNativeSubmit {
						submits++
						if r.Effect.Target != r.Resource.ID || r.Effect.Action != native.Delete || r.Resource.Kind == orchestration.NativeHerdrVolume {
							t.Fatal("unexpected teardown grant")
						}
						if submits == 2 {
							if scenario == "revoked-after-journal" {
								return environment.Binding{}, errors.New("fixture grant revoked")
							}
							if scenario == "replaced-after-grant" {
								i := f.project.instances["agent-1"]
								i.CreatedAt = i.CreatedAt.Add(time.Second)
								f.project.instances["agent-1"] = i
							}
						}
					}
					return enrolled.Binding, nil
				},
				CheckPolicy: orchestration.CheckPolicy{Mode: orchestration.ReadOnlyChecks, Authorize: func(context.Context, orchestration.TargetScope, check.Check) error { return nil }},
			}
			switch scenario {
			case "missing-policy":
				exec.AuthorizeTeardown = nil
			case "orphaned-state":
				plan.Resources = plan.Resources[:2]
			case "excess-pin":
				pins["omitted"] = pins["agent"]
			case "instance-pin":
				pins["agent"] = pins["agent"].Add(time.Second)
			case "volume-pin":
				selection.HerdrState = selection.HerdrState.Add(time.Second)
			}
			volume := f.project.volume
			engine := orchestration.NewEngine(nil)
			if scenario == "accepted" {
				r, err := exec.ExecuteLocalTeardown(ctx, engine, plan, selection, orchestration.ExecuteOptions{DryRun: true})
				if err != nil || r.Native == nil || len(r.Native.Previews) != 3 || len(r.Native.Effects) != 0 || r.Native.LocalOperationID != "" || submits != 0 || len(f.project.submissions) != 0 {
					t.Fatalf("teardown dry run mutated or lost its preview: %+v %v", r, err)
				}
			}
			r, err := exec.ExecuteLocalTeardown(ctx, engine, plan, selection, orchestration.ExecuteOptions{})
			if scenario == "accepted" {
				if err != nil || len(f.project.submissions) != 2 || submits != 4 || r.Native.LocalOperationID == "" || f.project.volume != volume {
					t.Fatalf("authorized teardown lost effects or storage: %+v %v", r, err)
				}
				return
			}
			if err == nil || len(f.project.submissions) != 0 || f.project.volume != volume {
				t.Fatalf("unsafe teardown: sdk=%v err=%v", f.project.submissions, err)
			}
			if scenario == "revoked-after-journal" || scenario == "replaced-after-grant" {
				op, err := store.ReadLocalNativeOperation(ctx, enrolled, r.Native.LocalOperationID)
				if err != nil || submits != 2 || op.Status != environment.LocalNativeFailed || len(op.Effects) != 1 || op.Effects[0].Outcome != environment.LocalNativeUncertain {
					t.Fatalf("lost interrupted teardown evidence: %+v %v", op, err)
				}
			} else if submits != 0 {
				t.Fatal("unsafe preflight reached submission authorization")
			}
		})
	}
}
