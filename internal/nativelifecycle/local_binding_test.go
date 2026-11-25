package nativelifecycle

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/blevinn/aginctus/internal/filestate"
	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/managementnetwork"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
	check "github.com/blevinn/aginctus/pkg/orchestration"
)

func lifecyclePlan(rev environment.AcceptedRevision, workloadID string) orchestration.NativePlan {
	return orchestration.NativePlan{APIVersion: orchestration.NativeAPIVersion, Kind: orchestration.NativePlanKind, Revision: rev, Resources: []orchestration.NativeResource{
		{ID: herdrclient.NativeInstanceID, Kind: orchestration.NativeHerdrInstance, DependsOn: []string{herdrclient.NativeVolumeID}},
		{ID: workloadID, Kind: orchestration.NativeWorkload, DependsOn: []string{ManagementNetworkID}},
		{ID: herdrclient.NativeVolumeID, Kind: orchestration.NativeHerdrVolume, DependsOn: []string{ManagementNetworkID}},
		{ID: ManagementNetworkID, Kind: orchestration.NativeManagementNetwork},
	}}
}

func TestJoinedTransportUsesAdvertisedStandaloneName(t *testing.T) {
	f := newFixture(t)
	f.project.serverName, f.project.info.Target = "standalone", "standalone"
	verify := func() error {
		return native.VerifyProjectTransport(t.Context(), f.project, f.accepted.Environment.Binding, f.pin)
	}
	if err := verify(); err != nil {
		t.Fatal(err)
	}
	f.project.info.Target = "other-member"
	if verify() == nil {
		t.Fatal("accepted a redirected target")
	}
	f.project.info.Target, f.project.clustered = f.project.serverName, true
	if verify() == nil {
		t.Fatal("accepted an unreviewed clustered connection")
	}
}

func TestLocalNativeJoinedWorkloadHerdr(t *testing.T) {
	for _, scenario := range []string{"joined", "revoked-after-journal", "missing-native", "bootstrap-not-accepted", "bootstrap-not-requested", "unexpected-edge", "mismatched-bridge", "stale-revision"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			f.accepted.Workloads[0].Native = &acceptedstate.WorkloadNativeSpec{Runtime: "pinned-runtime", StoragePool: "approved-pool", ManagementNetwork: "approved-bridge"}
			f.accepted.Herdr = &environment.HerdrSpec{InstanceName: "herdr-1", ImageFingerprint: f.project.fingerprint, StoragePool: "approved-pool", ManagementNetwork: "approved-bridge", AllowStateBootstrap: true}
			bootstrap := true
			switch scenario {
			case "missing-native":
				f.accepted.Workloads[0].Native = nil
			case "bootstrap-not-accepted":
				f.accepted.Herdr.AllowStateBootstrap = false
			case "bootstrap-not-requested":
				bootstrap = false
			case "mismatched-bridge":
				f.selection.Bridge.Spec.Name = "foreign-bridge"
			}
			ctx := t.Context()
			enrolled := f.accepted.Environment
			store, err := environment.InitializeLocalProjectStateStore(filepath.Join(t.TempDir(), "project"), enrolled.Binding)
			if err != nil {
				t.Fatal(err)
			}
			promote := func(_ context.Context, req environment.LocalPromotionRequest) (environment.Binding, error) {
				if !req.Environment.SameEnvironment(enrolled) || req.CandidateDigest == "" {
					t.Fatal("promotion lost its scope or digest")
				}
				return enrolled.Binding, nil
			}
			revision, err := store.PromoteLocalCandidate(ctx, enrolled, nil, f.accepted, promote)
			if err != nil {
				t.Fatal(err)
			}
			plan := lifecyclePlan(revision, "agent")
			if scenario == "unexpected-edge" {
				plan.Resources[0].DependsOn = append(plan.Resources[0].DependsOn, "agent")
			}
			if scenario == "stale-revision" {
				f.accepted.Herdr.AllowStateBootstrap = false
				if _, err := store.PromoteLocalCandidate(ctx, enrolled, &revision, f.accepted, promote); err != nil {
					t.Fatal(err)
				}
			}
			// Mutating the proposal after promotion cannot supply SDK coordinates.
			if f.accepted.Workloads[0].Native != nil {
				f.accepted.Workloads[0].Native.Runtime = "unaccepted-runtime"
			}
			f.accepted.Herdr.InstanceName = "unaccepted-name"
			var bridgeReads, startChecks int
			bridge := *f.selection.Bridge
			verifyBridge := bridge.VerifyBinding
			bridge.VerifyBinding = func(ctx context.Context, binding environment.Binding, spec managementnetwork.Spec) error {
				bridgeReads++
				return verifyBridge(ctx, binding, spec)
			}
			composer := LocalComposer{Environment: enrolled, Bridge: bridge, TrustedServerPin: f.pin, Client: f.project, VerifyAttachments: f.verify}
			dry := scenario == "joined"
			exec := orchestration.LocalNativeExecution{Store: store, Environment: enrolled, BindLifecycle: composer.BindLifecycle, BootstrapHerdr: bootstrap,
				CheckPolicy: orchestration.CheckPolicy{Mode: orchestration.ReadOnlyChecks, Authorize: func(_ context.Context, scope orchestration.TargetScope, c check.Check) error {
					if scope.Deployment != enrolled.ID || c.Effect != check.ReadOnly {
						t.Fatal("wrong check authority scope")
					}
					return nil
				}},
				Authorize: func(ctx context.Context, req orchestration.LocalNativeAuthorityRequest) (environment.Binding, error) {
					if req.Plan.Revision != revision || !req.Environment.SameEnvironment(enrolled) || req.BootstrapHerdr != bootstrap {
						t.Fatal("wrong authority request scope")
					}
					if req.Permission == orchestration.LocalNativeSubmit {
						if dry || req.Resource.ID != req.Effect.Target || req.Resource.ID == ManagementNetworkID {
							t.Fatalf("unexpected authority request: %+v", req)
						}
						if _, err := store.PinLocalRevision(ctx, enrolled, revision); !errors.Is(err, filestate.ErrRegistryBusy) {
							t.Fatalf("effect authority did not hold writer exclusion: %v", err)
						}
						if scenario == "revoked-after-journal" && req.Effect == (orchestration.NativeEffect{Target: herdrclient.NativeInstanceID, Action: native.Start}) {
							startChecks++
							if startChecks == 2 {
								return environment.Binding{}, errors.New("fixture grant revoked")
							}
						}
					}
					return enrolled.Binding, nil
				},
			}
			engine := orchestration.NewEngine(nil)
			if dry {
				r, err := exec.ExecuteLocalNative(ctx, engine, plan, orchestration.ExecuteOptions{DryRun: true})
				ids, readErr := store.ListLocalNativeOperationIDs(ctx, enrolled)
				if err != nil || readErr != nil || len(ids) != 0 || len(r.Native.Previews) != 4 || len(r.Conditions) != 0 || len(f.project.submissions) != 0 {
					t.Fatalf("joined dry run: result=%+v ids=%v errs=%v/%v", r, ids, err, readErr)
				}
				dry = false
			}
			r, err := exec.ExecuteLocalNative(ctx, engine, plan, orchestration.ExecuteOptions{})
			if scenario != "joined" && scenario != "revoked-after-journal" {
				if err == nil || len(f.project.submissions) != 0 || scenario != "bootstrap-not-requested" && bridgeReads != 0 {
					t.Fatalf("malformed or stale selection reached observation/effects: reads=%d writes=%v error=%v", bridgeReads, f.project.submissions, err)
				}
				return
			}
			if (err == nil) != (scenario == "joined") || r.Native == nil || len(r.Native.Effects) != 5 || r.Native.LocalOperationID == "" {
				t.Fatalf("joined result: %+v %v", r, err)
			}
			op, err := store.ReadLocalNativeOperation(ctx, enrolled, r.Native.LocalOperationID)
			if err != nil || op.Revision != revision || len(op.Effects) != 5 || f.project.instances["agent-1"].Config["user.aginctus.runtime"] != "pinned-runtime" {
				t.Fatalf("lost durable or pinned evidence: %+v %v", op, err)
			}
			want := []string{"create:agent-1", "start:agent-1", "create:herdr-1-state-system", "create:herdr-1", "start:herdr-1"}
			if scenario == "revoked-after-journal" {
				want = want[:4]
				if op.Status != environment.LocalNativeFailed || op.Effects[4].Outcome != environment.LocalNativeUncertain || op.Effects[4].Target != herdrclient.NativeInstanceID || startChecks != 2 {
					t.Fatalf("revocation did not preserve the pre-submission journal: %+v", op)
				}
			} else {
				if op.Status != environment.LocalNativeCompleted || len(r.Conditions) != 4 {
					t.Fatalf("lost joined observation or completion: %+v %+v", op, r.Conditions)
				}
				for i, e := range op.Effects {
					if e.Outcome != environment.LocalNativeApplied || e.Resource != r.Native.Effects[i].ResourceID || string(r.Native.Effects[i].Effect.Action) != e.Action {
						t.Fatalf("journal entry does not agree with engine evidence: %+v", e)
					}
				}
				// The second invocation should converge using the same retained
				// volume without bootstrap permission or any new mutation.
				volume := f.project.volume
				exec.BootstrapHerdr, bootstrap = false, false
				r, err := exec.ExecuteLocalNative(ctx, engine, plan, orchestration.ExecuteOptions{})
				if err != nil || len(r.Native.Effects) != 0 || f.project.volume != volume {
					t.Fatalf("retained-state retry: %+v %v", r, err)
				}
			}
			if !reflect.DeepEqual(want, f.project.submissions) {
				t.Fatalf("wrong SDK submissions: %v; want %v", f.project.submissions, want)
			}
		})
	}
}
