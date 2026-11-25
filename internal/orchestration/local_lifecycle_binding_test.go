package orchestration

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
	check "github.com/blevinn/aginctus/pkg/orchestration"
)

func TestLocalLifecycleDeniesUnpinnedAndMismatchedComposition(t *testing.T) {
	plan, original := nativePlanFixture(t)
	plan.Resources = []NativeResource{
		{ID: managementNetworkStage, Kind: NativeManagementNetwork},
		{ID: "agent", Kind: NativeWorkload, DependsOn: []string{managementNetworkStage}},
	}
	for _, name := range []string{"no-native-pin", "no-lifecycle-composer", "missing-bridge-edge", "extra-binding", "active-check"} {
		t.Run(name, func(t *testing.T) {
			p, c := detachLocalPlan(plan), original
			c.Workloads = append([]acceptedstate.WorkloadSpec{}, original.Workloads...)
			if name != "no-native-pin" {
				c.Workloads[0].Native = &acceptedstate.WorkloadNativeSpec{Runtime: "opencode", StoragePool: "pool", ManagementNetwork: "bridge"}
			}
			store, err := environment.InitializeLocalProjectStateStore(filepath.Join(t.TempDir(), "store"), c.Environment.Binding)
			if err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			p.Revision, err = store.CommitLocalCandidate(ctx, c.Environment, c.Environment.Binding, nil, c)
			if err != nil {
				t.Fatal(err)
			}
			f := LocalNativeExecution{Store: store, Environment: c.Environment,
				Authorize: func(context.Context, LocalNativeAuthorityRequest) (environment.Binding, error) {
					return c.Environment.Binding, nil
				},
				Bind: func(environment.Candidate, NativePlan) (map[string]NativeResourceBinding, error) {
					t.Fatal("lifecycle selection went to gateway binder")
					return nil, nil
				},
				BindLifecycle: func(_ environment.Candidate, built NativePlan, _ bool) (NativePlan, map[string]NativeResourceBinding, error) {
					if name == "no-native-pin" {
						t.Fatal("incomplete pin reached binder")
					}
					binding := NativeResourceBinding{
						Validate: func() error { t.Fatal("unsafe composition reached engine"); return nil },
						Preview: func(context.Context) (NativePreview, error) {
							t.Fatal("unsafe composition reached observation")
							return NativePreview{}, nil
						},
					}
					if name == "active-check" {
						binding.Conditions.Postconditions = []BoundCheck{{Check: check.Check{Effect: check.ActiveProbe}}}
					}
					bindings := map[string]NativeResourceBinding{"agent": binding, managementNetworkStage: binding}
					if name == "extra-binding" {
						bindings["extra"] = binding
					}
					if name == "missing-bridge-edge" {
						built.Resources[1].DependsOn = nil
					}
					return built, bindings, nil
				},
			}
			if name == "no-lifecycle-composer" {
				f.BindLifecycle = nil
			}
			if _, err := f.ExecuteLocalNative(ctx, NewEngine(nil), p, ExecuteOptions{DryRun: true}); err == nil {
				t.Fatal("unsafe composition accepted")
			}
		})
	}
}

func TestLocalHerdrBootstrapRequiresPinnedAndInvocationConsent(t *testing.T) {
	_, c := nativePlanFixture(t)
	c.Herdr = &environment.HerdrSpec{InstanceName: "herdr", ImageFingerprint: strings.Repeat("f", 64), StoragePool: "pool", ManagementNetwork: "bridge"}
	bridge := NativeResource{ID: managementNetworkStage, Kind: NativeManagementNetwork}
	volume := NativeResource{ID: herdrVolumeStage, Kind: NativeHerdrVolume, DependsOn: []string{bridge.ID}}
	instance := NativeResource{ID: herdrInstanceStage, Kind: NativeHerdrInstance, DependsOn: []string{volume.ID}}
	resources := []NativeResource{bridge, volume, instance}
	create := NativeEffect{native.Create, volume.ID}
	if verifyLocalNativeSelection(c, resources, false) != nil || verifyLocalNativeSelection(c, resources, true) == nil || localEffectSelected(c, volume, create, true) {
		t.Fatal("retention-only accepted intent enabled bootstrap")
	}
	c.Herdr.AllowStateBootstrap = true
	if verifyLocalNativeSelection(c, resources, true) != nil || localEffectSelected(c, volume, create, false) || !localEffectSelected(c, volume, create, true) {
		t.Fatal("bootstrap did not require both accepted and invocation consent")
	}
	if localEffectSelected(c, volume, NativeEffect{native.Delete, volume.ID}, true) || localEffectSelected(c, instance, NativeEffect{native.Delete, instance.ID}, true) || localEffectSelected(c, instance, NativeEffect{NativeStop, instance.ID}, true) || localEffectSelected(c, bridge, NativeEffect{native.Create, bridge.ID}, true) {
		t.Fatal("reconcile granted a destructive or host mutation effect")
	}
	if verifyLocalNativeSelection(c, resources[:2], true) == nil || verifyLocalNativeSelection(c, []NativeResource{bridge, instance}, false) == nil {
		t.Fatal("orphan Herdr stage accepted")
	}
	c.Workloads[0].Native = &acceptedstate.WorkloadNativeSpec{Runtime: "opencode", StoragePool: "root", ManagementNetwork: "foreign"}
	if verifyLocalNativeSelection(c, append(resources, NativeResource{ID: "agent", Kind: NativeWorkload, DependsOn: []string{bridge.ID}}), false) == nil {
		t.Fatal("single management stage could silently select a different pinned network")
	}
}
