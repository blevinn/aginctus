package orchestration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
	check "github.com/blevinn/aginctus/pkg/orchestration"
	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

func nativePlanFixture(t *testing.T) (NativePlan, environment.Candidate) {
	t.Helper()
	c := environment.Candidate{
		APIVersion: environment.CandidateAPIVersion, Kind: environment.CandidateKind,
		Environment:       environment.Descriptor{ID: "a5332177-6d41-4160-a63d-b1ead7b31527", Name: "test", Binding: environment.Binding{ServerID: "sha256:" + strings.Repeat("a", 64), Project: "selected"}},
		SelectedWorkloads: []string{"agent"},
		Workloads:         []acceptedstate.WorkloadSpec{{ID: "agent", InstanceName: "agent", InstanceType: "container", ImageFingerprint: strings.Repeat("b", 64), Profiles: []string{}, EnsureRunning: true}},
	}
	r, err := environment.BuildAcceptedRevision(nil, nil, c)
	if err != nil {
		t.Fatal(err)
	}
	return NativePlan{NativeAPIVersion, NativePlanKind, r, []NativeResource{{ID: "agent", Kind: NativeWorkload}}}, c
}

// This composes the existing accepted-state comparison and lifecycle reconciler
// through the engine. Only the authority and Incus transport are fixtures.
func TestNativeEngineWithExistingWorkloadReconciler(t *testing.T) {
	p, accepted := nativePlanFixture(t)
	w := accepted.Workloads[0]
	client := &fixtureLifecycleClient{instance: api.Instance{
		Name: w.InstanceName, Project: p.Revision.Project, Type: w.InstanceType, Status: "Stopped", CreatedAt: time.Now(),
		InstancePut: api.InstancePut{Profiles: w.Profiles, Config: map[string]string{
			"user.aginctus.managed": "true", "user.aginctus.resource": "workload", "user.aginctus.role": "agent-workload",
			"user.aginctus.workload": w.ID, "user.aginctus.environment-id": p.Revision.EnvironmentID, "volatile.base_image": w.ImageFingerprint,
		}},
	}}
	scope, err := incusscope.NewProjectScope(p.Revision.Project)
	if err != nil {
		t.Fatal(err)
	}
	intent := native.InstanceLifecycleIntent{Scope: scope, Name: w.InstanceName, Role: "agent-workload", Resource: "workload", WorkloadID: w.ID, EnvironmentID: p.Revision.EnvironmentID, State: native.InstanceRunning}
	b := NativeBindings{
		VerifyPlan: func(_ context.Context, selected NativePlan) error { return selected.Revision.ValidateAgainst(accepted) },
		BeforeEffect: func(context.Context, environment.AcceptedRevision, NativeResource, NativeEffect) error {
			client.guards++
			return nil
		},
		Resources: map[string]NativeResourceBinding{w.ID: {
			Validate: intent.Validate,
			Preview: func(context.Context) (NativePreview, error) {
				i := client.instance
				a, err := native.AssessPinnedAcceptedWorkloads(accepted.Environment, p.Revision, accepted, []native.AcceptedWorkloadObservation{{
					WorkloadID: w.ID, Instance: &native.InstanceObservation{Identity: native.InstanceIdentity{Project: i.Project, Name: i.Name},
						Type: i.Type, State: i.Status, Config: i.Config, Profiles: i.Profiles, ImageFingerprint: i.Config["volatile.base_image"]},
				}})
				if err != nil {
					return NativePreview{}, err
				}
				switch result := a.Workloads[0]; {
				case result.Status == "converged":
					return NativePreview{Actions: []NativeEffect{{native.Unchanged, w.ID}}}, nil
				case reflect.DeepEqual(result.Fields, []string{"ensureRunning"}):
					return NativePreview{Actions: []NativeEffect{{NativeStart, w.ID}}}, nil
				default:
					return NativePreview{Actions: []NativeEffect{{native.ReplacementRequired, w.ID}}}, nil
				}
			},
			Apply: func(ctx context.Context, run RunNativeEffect) error {
				_, err := native.EnsureInstanceStateWithClient(ctx, intent, effectLifecycleClient{client, run})
				return err
			},
		}},
	}
	e := NewEngine(nil)
	result, err := e.ExecuteNative(t.Context(), p, b, ExecuteOptions{DryRun: true})
	if err != nil || client.writes != 0 || client.guards != 0 || !reflect.DeepEqual(result.Native.Previews[0].Actions, []NativeEffect{{NativeStart, w.ID}}) {
		t.Fatalf("preview: %+v %v", result, err)
	}
	result, err = e.ExecuteNative(t.Context(), p, b, ExecuteOptions{})
	if err != nil || client.writes != 1 || client.guards != 1 || result.Native.Effects[0].Outcome != NativeEffectApplied || client.instance.Status != "Running" {
		t.Fatalf("apply: %+v %v", result, err)
	}
	result, err = e.ExecuteNative(t.Context(), p, b, ExecuteOptions{})
	if err != nil || client.writes != 1 || client.guards != 1 || len(result.Native.Effects) != 0 {
		t.Fatalf("repeated apply: %+v %v", result, err)
	}
	client.instance.Config["user.aginctus.environment-id"] = "foreign"
	if _, err := e.ExecuteNative(t.Context(), p, b, ExecuteOptions{}); err == nil || client.writes != 1 {
		t.Fatalf("foreign workload did not fail preflight: %v", err)
	}
}

type fixtureLifecycleClient struct {
	instance       api.Instance
	writes, guards int
}

func (c *fixtureLifecycleClient) GetConnectionInfo() (*incus.ConnectionInfo, error) {
	return &incus.ConnectionInfo{Protocol: "incus", SocketPath: "/fixture/incus.sock", Project: c.instance.Project}, nil
}

func (c *fixtureLifecycleClient) GetInstance(name string) (*api.Instance, string, error) {
	if name != c.instance.Name {
		return nil, "", errors.New("unexpected fixture instance selection")
	}
	i := c.instance
	return &i, "test-etag", nil
}

type effectLifecycleClient struct {
	*fixtureLifecycleClient
	run RunNativeEffect
}

type fixtureWait func(context.Context) error

func (f fixtureWait) WaitContext(ctx context.Context) error { return f(ctx) }

func (c effectLifecycleClient) UpdateInstanceState(name string, put api.InstanceStatePut, etag string) (native.InstanceOperation, error) {
	return fixtureWait(func(context.Context) error {
		return c.run(NativeEffect{NativeStart, name}, func(context.Context) error {
			if name != c.instance.Name || put.Action != "start" || etag != "test-etag" || c.guards != c.writes+1 {
				return errors.New("unverified fixture submission")
			}
			c.writes++
			c.instance.Status = "Running"
			return nil
		})
	}), nil
}

func TestNativePlanRejectsAmbiguousJSONAndMalformedGraphs(t *testing.T) {
	p, _ := nativePlanFixture(t)
	p.Resources = append(p.Resources, NativeResource{ID: "db", Kind: NativeGatewayVolume, DependsOn: []string{"agent"}})
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ParseNativePlan(raw); err != nil || !reflect.DeepEqual(got, p) {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	for _, raw := range [][]byte{
		append(bytes.Clone(raw), raw...),
		bytes.ReplaceAll(raw, []byte(`"resources":`), []byte(`"Resources":`)),
		bytes.ReplaceAll(raw, []byte(`"project":`), []byte(`"project":"shadow","project":`)),
		bytes.ReplaceAll(raw, []byte(`"project":`), []byte(`"project":null,"project":`)),
		bytes.ReplaceAll(raw, []byte(`"workload"`), []byte(`"shell"`)),
		bytes.ReplaceAll(raw, []byte(`"workload"`), []byte(`"workload","project":"shadow"`)),
		bytes.ReplaceAll(raw, []byte(`"selected"`), []byte(`"default"`)),
	} {
		if _, err := ParseNativePlan(raw); err == nil {
			t.Fatalf("accepted malformed plan: %s", raw)
		}
	}
	for _, deps := range [][]string{{"missing"}, {"agent"}, {"db"}, {"db", "db"}} {
		p.Resources[0].DependsOn = deps
		if err := p.Validate(); err == nil {
			t.Fatalf("accepted graph with dependencies %v", deps)
		}
	}
}

func nativeCheck(plan NativePlan, id, target string, preflight bool, f func() check.Outcome) BoundCheck {
	return BoundCheck{
		Check:     check.Check{ID: id, Claim: "verify the selected target", Scope: "one selected target", Effect: check.Offline, Required: true, Timeout: time.Second},
		Preflight: preflight,
		Evaluate: func(context.Context) (check.Finding, error) {
			return check.Finding{TargetID: target, CheckID: id, ObservedAt: time.Now(), Code: "observed", AcceptedRevision: plan.Revision.ID, Outcome: f()}, nil
		},
	}
}

func TestNativeDryRunOnlyChecksFeasibleReadPreflight(t *testing.T) {
	p, _ := nativePlanFixture(t)
	var evaluated, authorized int
	pre := nativeCheck(p, "preflight", "agent", true, func() check.Outcome {
		evaluated++
		return check.Passed
	})
	pre.Check.Effect, pre.Check.Access = check.ReadOnly, []check.Access{check.IncusAccess}
	post := nativeCheck(p, "ready", "agent", false, func() check.Outcome {
		t.Fatal("attempted readiness in dry-run")
		return check.Passed
	})
	post.Check.Effect, post.Check.Access, post.Check.OptIns = check.ActiveProbe, []check.Access{check.GuestAccess}, []string{"probe"}
	b := NativeBindings{
		VerifyPlan: func(context.Context, NativePlan) error { return nil },
		CheckPolicy: CheckPolicy{Mode: ReadOnlyChecks, Authorize: func(_ context.Context, scope TargetScope, c check.Check) error {
			if scope.Deployment != p.Revision.EnvironmentID || c.ID != "preflight" {
				t.Fatalf("unexpected check authorization: %+v %+v", scope, c)
			}
			authorized++
			return nil
		}},
		Resources: map[string]NativeResourceBinding{"agent": {
			Validate: func() error { return nil },
			Preview: func(context.Context) (NativePreview, error) {
				return NativePreview{Actions: []NativeEffect{{native.Unchanged, "agent"}}}, nil
			},
			Conditions: StepConditions{Preconditions: []BoundCheck{pre}, Postconditions: []BoundCheck{post}},
		}},
	}
	result, err := NewEngine(nil).ExecuteNative(t.Context(), p, b, ExecuteOptions{DryRun: true})
	if err != nil || evaluated != 1 || authorized != 2 || len(result.Conditions) != 1 || result.Conditions[0].Phase != PreflightPhase {
		t.Fatalf("dry-run checks: evaluated=%d authorized=%d %+v %v", evaluated, authorized, result, err)
	}
}

func TestNativeEngineOrderAndPartialEvidence(t *testing.T) {
	for _, dry := range []bool{true, false} {
		t.Run(map[bool]string{true: "dry-run", false: "apply"}[dry], func(t *testing.T) {
			p, _ := nativePlanFixture(t)
			p.Resources = []NativeResource{
				{ID: "agent", Kind: NativeWorkload, DependsOn: []string{"image"}},
				{ID: "image", Kind: NativeGatewayImage},
				{ID: "db", Kind: NativeGatewayVolume},
			}
			var calls []string
			imageReady := false
			b := NativeBindings{Resources: map[string]NativeResourceBinding{}, VerifyPlan: func(_ context.Context, plan NativePlan) error {
				calls = append(calls, "pin")
				if plan.Revision != p.Revision || plan.Resources[2].DependsOn[0] != "image" {
					t.Fatal("incorrect authority input")
				}
				plan.Resources[2].DependsOn[0] = "not-selected"
				return nil
			}, BeforeEffect: func(_ context.Context, rev environment.AcceptedRevision, resource NativeResource, effect NativeEffect) error {
				calls = append(calls, "guard:"+resource.ID+":"+string(effect.Action))
				if rev != p.Revision || resource.ID != effect.Target || resource.ID == "agent" && !reflect.DeepEqual(resource.DependsOn, []string{"image"}) {
					t.Fatal("authority input lost its exact binding")
				}
				return nil
			}}
			for _, r := range p.Resources {
				action := native.Create
				if r.ID == "agent" {
					action = NativeStart
				}
				binding := NativeResourceBinding{
					Validate: func() error { calls = append(calls, "validate:"+r.ID); return nil },
					Preview: func(context.Context) (NativePreview, error) {
						calls = append(calls, "preview:"+r.ID)
						if r.ID == "agent" && !imageReady {
							return NativePreview{Deferred: true}, nil
						}
						return NativePreview{Actions: []NativeEffect{{action, r.ID}}}, nil
					},
					Apply: func(_ context.Context, run RunNativeEffect) error {
						return run(NativeEffect{action, r.ID}, func(context.Context) error {
							calls = append(calls, "write:"+r.ID)
							return nil
						})
					},
				}
				if r.ID == "image" {
					binding.Conditions.Postconditions = []BoundCheck{nativeCheck(p, "verified", r.ID, false, func() check.Outcome {
						calls = append(calls, "verified:image")
						imageReady = true
						return check.Passed
					})}
				}
				if r.ID == "agent" {
					binding.Conditions.Postconditions = []BoundCheck{nativeCheck(p, "ready", r.ID, false, func() check.Outcome {
						calls = append(calls, "ready:agent")
						return check.Unknown
					})}
				}
				b.Resources[r.ID] = binding
			}
			result, err := NewEngine(nil).ExecuteNative(t.Context(), p, b, ExecuteOptions{DryRun: dry, Reporting: true})
			want := []string{"validate:db", "validate:image", "validate:agent", "pin", "preview:db", "preview:image", "preview:agent"}
			if dry {
				if err != nil || len(result.Operations) != 0 || len(result.Native.Effects) != 0 || len(result.Conditions) != 0 {
					t.Fatalf("dry run: %+v %v", result, err)
				}
			} else {
				want = append(want, "preview:db", "guard:db:create", "write:db", "preview:image", "guard:image:create", "write:image", "verified:image", "preview:agent", "guard:agent:start", "write:agent", "ready:agent")
				if !errors.Is(err, ErrConditionNotMet) || len(result.Operations) != 3 || len(result.Steps) != 2 || len(result.Native.Effects) != 3 || len(result.Conditions) != 2 {
					t.Fatalf("apply lost completed-stage evidence: %+v %v", result, err)
				}
			}
			if result.Report == nil || !result.Native.Previews[2].Deferred || !reflect.DeepEqual(calls, want) {
				t.Fatalf("calls = %v; want %v; result = %+v", calls, want, result)
			}
		})
	}
}

func TestNativeEngineFailsClosed(t *testing.T) {
	for _, mode := range []string{"validation", "pin", "collision", "check-policy", "dependency-blocked", "still-deferred", "denied", "cancel", "unplanned"} {
		t.Run(mode, func(t *testing.T) {
			p, _ := nativePlanFixture(t)
			p.Resources = append(p.Resources, NativeResource{ID: "other", Kind: NativeWorkload, DependsOn: []string{"agent"}})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			sentinel := errors.New("test denial")
			writes := 0
			var retained RunNativeEffect
			primary := NativeResourceBinding{
				Validate: func() error { return nil },
				Preview: func(context.Context) (NativePreview, error) {
					return NativePreview{Actions: []NativeEffect{{native.Update, "agent"}, {NativeStart, "agent"}}}, nil
				},
				Apply: func(_ context.Context, run RunNativeEffect) error {
					retained = run
					// A buggy trusted adapter ignoring an effect error cannot resume.
					_ = run(NativeEffect{native.Update, "agent"}, func(context.Context) error { writes++; return nil })
					_ = run(NativeEffect{NativeStart, "agent"}, func(context.Context) error {
						writes++
						if mode == "cancel" {
							cancel()
						}
						return nil
					})
					_ = run(NativeEffect{NativeStart, "other"}, func(context.Context) error { t.Fatal("unplanned target submitted"); return nil })
					return nil
				},
				Conditions: StepConditions{Postconditions: []BoundCheck{nativeCheck(p, "verified", "agent", false, func() check.Outcome {
					if mode == "dependency-blocked" {
						return check.Failed
					}
					return check.Passed
				})}},
			}
			// Allow only one action in the modes that need to reach a postcondition.
			if mode == "dependency-blocked" || mode == "still-deferred" {
				primary.Preview = func(context.Context) (NativePreview, error) {
					return NativePreview{Actions: []NativeEffect{{NativeStart, "agent"}}}, nil
				}
				primary.Apply = func(_ context.Context, run RunNativeEffect) error {
					return run(NativeEffect{NativeStart, "agent"}, func(context.Context) error { writes++; return nil })
				}
			}
			b := NativeBindings{Resources: map[string]NativeResourceBinding{"agent": primary}, VerifyPlan: func(context.Context, NativePlan) error {
				if mode == "pin" {
					return sentinel
				}
				return nil
			}, BeforeEffect: func(_ context.Context, _ environment.AcceptedRevision, _ NativeResource, effect NativeEffect) error {
				if mode == "denied" && effect.Action == NativeStart {
					return sentinel
				}
				return nil
			}}
			b.Resources["other"] = NativeResourceBinding{
				Validate: func() error {
					if mode == "validation" {
						return sentinel
					}
					return nil
				},
				Preview: func(context.Context) (NativePreview, error) {
					if mode == "collision" {
						return NativePreview{}, sentinel
					}
					if mode == "still-deferred" {
						return NativePreview{Deferred: true}, nil
					}
					return NativePreview{Actions: []NativeEffect{{native.Unchanged, "other"}}}, nil
				},
				Apply: func(context.Context, RunNativeEffect) error { t.Fatal("dependent apply called"); return nil },
			}
			if mode == "check-policy" {
				active := nativeCheck(p, "active", "agent", true, func() check.Outcome { t.Fatal("active dry-run probe called"); return check.Passed })
				active.Check.Effect, active.Check.Access, active.Check.OptIns = check.ActiveProbe, []check.Access{check.GuestAccess}, []string{"probe"}
				primary.Conditions.Preconditions = []BoundCheck{active}
				b.Resources["agent"] = primary
			}
			result, err := NewEngine(nil).ExecuteNative(ctx, p, b, ExecuteOptions{DryRun: mode == "check-policy"})
			if err == nil {
				t.Fatal("failed closed case succeeded")
			}
			if retained != nil && retained(NativeEffect{NativeStart, "agent"}, func(context.Context) error { t.Fatal("expired capability wrote"); return nil }) == nil {
				t.Fatal("expected expiry error")
			}
			count := 0
			switch mode {
			case "denied", "cancel", "unplanned":
				count = 2
				want := NativeEffectApplied
				if mode == "denied" {
					count = 1
					want = NativeEffectBlocked
				}
				if mode == "cancel" {
					want = NativeEffectUncertain
				}
				if len(result.Native.Effects) != 2 || result.Native.Effects[0].Outcome != NativeEffectApplied || result.Native.Effects[1].Outcome != want || len(result.Steps) != 0 {
					t.Fatalf("unexpected partial outcome: %+v", result.Native)
				}
			case "dependency-blocked", "still-deferred":
				count = 1
			}
			if writes != count || mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("writes=%d/%d; %v", writes, count, err)
			}
		})
	}
}
