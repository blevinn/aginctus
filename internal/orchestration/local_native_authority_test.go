package orchestration

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/blevinn/aginctus/internal/filestate"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

func TestLocalNativeJournalAbruptExit(t *testing.T) {
	plan, c := nativePlanFixture(t)
	ctx := t.Context()
	if root := os.Getenv("AGINCTUS_NATIVE_JOURNAL_TEST_ROOT"); root != "" {
		store, err := environment.OpenLocalProjectStateStore(root, c.Environment.Binding)
		if err != nil {
			t.Fatal(err)
		}
		pin, err := store.PinLocalRevision(ctx, c.Environment, plan.Revision)
		if err != nil {
			t.Fatal(err)
		}
		journal, err := pin.NewLocalNativeJournal()
		if err != nil {
			t.Fatal(err)
		}
		if err := journal.BeginEffect(ctx, "agent", "agent", "update"); err != nil {
			t.Fatal(err)
		}
		if err := journal.MarkApplied(); err != nil {
			t.Fatal(err)
		}
		if err := journal.BeginEffect(ctx, "agent", "agent", "start"); err != nil {
			t.Fatal(err)
		}
		os.Exit(81) // No defers and no handoff of the operation ID.
	}
	root := filepath.Join(t.TempDir(), "project")
	store, err := environment.InitializeLocalProjectStateStore(root, c.Environment.Binding)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitLocalCandidate(ctx, c.Environment, c.Environment.Binding, nil, c); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLocalNativeJournalAbruptExit$")
	cmd.Env = append(os.Environ(), "AGINCTUS_NATIVE_JOURNAL_TEST_ROOT="+root)
	var exit *exec.ExitError
	if data, err := cmd.CombinedOutput(); !errors.As(err, &exit) || exit.ExitCode() != 81 {
		t.Fatalf("child failed before durable journal: %v %s", err, data)
	}
	ids, err := store.ListLocalNativeOperationIDs(ctx, c.Environment)
	if err != nil || len(ids) != 1 {
		t.Fatalf("cannot discover abruptly interrupted operation: %v %v", ids, err)
	}
	op, err := store.ReadLocalNativeOperation(ctx, c.Environment, ids[0])
	if err != nil || op.Revision != plan.Revision || len(op.Effects) != 2 ||
		op.Effects[0].Outcome != environment.LocalNativeApplied || op.Effects[1].Outcome != environment.LocalNativeUncertain {
		t.Fatalf("crash recovery lost applied or uncertain stages: %+v %v", op, err)
	}
	pin, err := store.PinLocalRevision(ctx, c.Environment, plan.Revision)
	if err != nil {
		t.Fatalf("dead process still excludes cooperating writers: %v", err)
	}
	if err := pin.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLocalNativeGatewaySelectionRejectsForeignTargets(t *testing.T) {
	_, c := nativePlanFixture(t)
	c.Gateway = &environment.GatewaySpec{EnsureRunning: true}
	images := NativeResource{ID: GatewayImageStage, Kind: NativeGatewayImage}
	volume := NativeResource{ID: GatewayVolumeStage, Kind: NativeGatewayVolume, DependsOn: []string{GatewayImageStage}}
	instances := NativeResource{ID: GatewayInstanceStage, Kind: NativeGatewayInstance, DependsOn: []string{GatewayVolumeStage}}
	runtime := NativeResource{ID: GatewayRuntimeStage, Kind: NativeGatewayRuntime, DependsOn: []string{GatewayInstanceStage}}
	if err := verifyLocalNativeSelection(c, []NativeResource{images, volume, instances, runtime}); err != nil {
		t.Fatal(err)
	}
	if localEffectSelected(c, runtime, NativeEffect{NativeStop, GatewayPostgresTarget}) ||
		localEffectSelected(c, runtime, NativeEffect{NativeStart, "foreign"}) ||
		localEffectSelected(c, volume, NativeEffect{native.Create, GatewayPostgresTarget}) ||
		!localEffectSelected(c, runtime, NativeEffect{NativeStart, GatewayPostgresTarget}) ||
		!localEffectSelected(c, volume, NativeEffect{native.Create, GatewayDatabaseTarget}) {
		t.Fatal("gateway operation was not constrained to accepted runtime state and specific targets")
	}
	instances.DependsOn = nil
	if verifyLocalNativeSelection(c, []NativeResource{images, volume, instances, runtime}) == nil {
		t.Fatal("accepted an omitted required edge")
	}
	c.Gateway = nil
	if verifyLocalNativeSelection(c, []NativeResource{images}) == nil {
		t.Fatal("gateway absent but selected")
	}
}

func TestLocalNativeAuthorityAndRecovery(t *testing.T) {
	ctx := t.Context()
	plan, c := nativePlanFixture(t)
	root := filepath.Join(t.TempDir(), "store")
	store, err := environment.InitializeLocalProjectStateStore(root, c.Environment.Binding)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitLocalCandidate(ctx, c.Environment, c.Environment.Binding, nil, c); err != nil {
		t.Fatal(err)
	}
	changed := c
	changed.Environment.Name = "new-name"
	var writes, submitChecks, previews int
	var denyAfterEffect bool
	authority := LocalNativeExecution{
		Store: store, Environment: c.Environment,
		Authorize: func(_ context.Context, r LocalNativeAuthorityRequest) (environment.Binding, error) {
			if !sameLocalPlan(r.Plan, plan) || !r.Environment.SameEnvironment(c.Environment) {
				t.Fatal("bad request scope")
			}
			// If trusted policy accidentally modifies its copy, execution stays pinned.
			r.Plan.Resources[0].ID = "other"
			if r.Permission == LocalNativeSubmit {
				submitChecks++
				if r.Effect.Target != "agent" || r.Resource.ID != "agent" {
					t.Fatal("wrong effect scope")
				}
				if denyAfterEffect && r.Effect.Action == NativeStart && submitChecks == 4 {
					return environment.Binding{}, errors.New("revoked")
				}
			}
			return c.Environment.Binding, nil
		},
		Bind: func(candidate environment.Candidate, p NativePlan) (map[string]NativeResourceBinding, error) {
			if p.Revision.ValidateAgainst(candidate) != nil {
				t.Fatal("binding received unpinned candidate")
			}
			return map[string]NativeResourceBinding{"agent": {
				Validate: func() error { return nil },
				Preview: func(context.Context) (NativePreview, error) {
					previews++
					return NativePreview{Actions: []NativeEffect{{native.Update, "agent"}, {NativeStart, "agent"}}}, nil
				},
				Apply: func(ctx context.Context, run RunNativeEffect) error {
					if _, err := store.CommitLocalCandidate(ctx, c.Environment, c.Environment.Binding, &p.Revision, changed); !errors.Is(err, filestate.ErrRegistryBusy) {
						t.Fatalf("CAS escaped exclusion: %v", err)
					}
					if _, err := store.PinLocalRevision(ctx, c.Environment, p.Revision); !errors.Is(err, filestate.ErrRegistryBusy) {
						t.Fatalf("competing execute escaped exclusion: %v", err)
					}
					for _, effect := range []NativeEffect{{native.Update, "agent"}, {NativeStart, "agent"}} {
						if err := run(effect, func(ctx context.Context) error {
							writes++
							ids, err := store.ListLocalNativeOperationIDs(ctx, c.Environment)
							if err != nil || len(ids) != 1 {
								t.Fatalf("submission lacked discoverable journal: %v %v", ids, err)
							}
							pending, err := store.ReadLocalNativeOperation(ctx, c.Environment, ids[0])
							if err != nil || len(pending.Effects) != writes || pending.Effects[writes-1].Outcome != environment.LocalNativeUncertain {
								t.Fatalf("submission lacked durable intent: %+v %v", pending, err)
							}
							return nil
						}); err != nil {
							return err
						}
					}
					return nil
				},
			}}, nil
		},
	}
	engine := NewEngine(nil)
	result, err := authority.ExecuteLocalNative(ctx, engine, plan, ExecuteOptions{DryRun: true})
	if err != nil || writes != 0 || submitChecks != 0 || previews != 1 || result.Native.LocalOperationID != "" {
		t.Fatalf("dry run: %+v %v", result, err)
	}
	lock := filepath.Join(root, "environments", c.Environment.ID, "active.json.flock")
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if _, err := authority.ExecuteLocalNative(ctx, engine, plan, ExecuteOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lock); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry run created a lock file: %v", err)
	}
	denyAfterEffect = true
	result, err = authority.ExecuteLocalNative(ctx, engine, plan, ExecuteOptions{})
	if err == nil || writes != 1 || submitChecks != 4 || result.Native.LocalOperationID == "" || len(result.Native.Effects) != 2 ||
		result.Native.Effects[0].Outcome != NativeEffectApplied || result.Native.Effects[1].Outcome != NativeEffectUncertain {
		t.Fatalf("partial outcome: %+v, %v writes=%d submitChecks=%d", result.Native, err, writes, submitChecks)
	}
	if _, err := store.CommitLocalCandidate(ctx, c.Environment, c.Environment.Binding, &plan.Revision, changed); err != nil {
		t.Fatal(err)
	}
	reopened, err := environment.OpenLocalProjectStateStore(root, c.Environment.Binding)
	if err != nil {
		t.Fatal(err)
	}
	op, err := reopened.ReadLocalNativeOperation(ctx, c.Environment, result.Native.LocalOperationID)
	if err != nil || op.Revision != plan.Revision || !slices.EqualFunc(op.Effects, []environment.LocalNativeOutcome{environment.LocalNativeApplied, environment.LocalNativeUncertain}, func(r environment.LocalNativeEffectRecord, v environment.LocalNativeOutcome) bool {
		return r.Outcome == v
	}) {
		t.Fatalf("recovery: %+v %v", op, err)
	}
	if _, err := authority.ExecuteLocalNative(ctx, engine, plan, ExecuteOptions{}); !errors.Is(err, environment.ErrAcceptedConflict) {
		t.Fatalf("stale execution: %v", err)
	}
}
