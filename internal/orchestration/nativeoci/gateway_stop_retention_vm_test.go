package nativeoci_test

import (
	"context"
	"errors"
	"io"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
)

func TestNativeGatewayStopRetentionVM(t *testing.T) {
	ctx, admin, binding := openGatewayFixtureVM(t, "aginctus-native-stop-retention")
	createGatewayFixtureBridges(t, admin)
	sdk := admin.UseProject(binding.Project)
	executable, err := os.ReadFile(os.Getenv("AGINCTUS_STOP_FIXTURE"))
	if err != nil || len(executable) == 0 {
		t.Fatalf("missing static fixture executable: %v", err)
	}
	images := gateway.NativeGatewayCreateImages{
		Postgres: importGatewayFixtureImage(t, ctx, sdk, binding.Project, "postgres", executable),
		LiteLLM:  importGatewayFixtureImage(t, ctx, sdk, binding.Project, "litellm", executable),
	}
	bootPair := func(t *testing.T, id, egress string) (gateway.NativeGatewayTopology, gateway.NativeGatewayDevices, gateway.NativeGatewayInstancePair) {
		t.Helper()
		desc := environment.Descriptor{ID: id, Name: "stop-retention-fixture", Binding: binding}
		top, err := gateway.BindNativeGatewayTopology(stoppedPairCandidate(binding.Project, images, egress), desc, binding)
		if err != nil {
			t.Fatal(err)
		}
		devices, err := gateway.PlanNativeGatewayDevices(top, stoppedPairPool, stoppedPairPool)
		if err != nil {
			t.Fatal(err)
		}
		pair, err := gateway.PlanNativeGatewayInstances(top, devices, images.Postgres.Evidence.Alias, images.LiteLLM.Evidence.Alias)
		if err != nil {
			t.Fatal(err)
		}
		// Only the disposable test is a writer. This guard is not a production fence.
		volume, err := gateway.EnsureNativeEnvironmentGatewayVolumeWithClient(ctx, desc, binding, stoppedPairPool, sdk, context.Context.Err)
		if err != nil || !volume.Created || !volume.Verified || volume.Name != top.DatabaseVolume {
			t.Fatalf("create fixture volume: %+v: %v", volume, err)
		}
		created, err := gateway.CreateStoppedGatewayWithClient(ctx, top, devices, pair, images, &stoppedPairSDK{InstanceServer: sdk}, context.Context.Err)
		want := []string{pair.Postgres.Name, pair.LiteLLM.Name}
		if err != nil || !slices.Equal(created.Created, want) || !slices.Equal(created.Verified, want) {
			t.Fatalf("create stopped fixture: %+v: %v (%v)", created, err, errors.Unwrap(err))
		}
		identity, err := gateway.NativeGatewayIdentity(desc, binding)
		if err != nil {
			t.Fatal(err)
		}
		revision, err := environment.BuildAcceptedRevision(nil, nil, environment.Candidate{
			APIVersion: environment.CandidateAPIVersion, Kind: environment.CandidateKind, Environment: desc,
			SelectedWorkloads: []string{}, Workloads: []acceptedstate.WorkloadSpec{},
		})
		if err != nil {
			t.Fatal(err)
		}
		runtime := gateway.NativeGatewayRuntimeBinding{Resources: identity, AcceptedEnvironmentRevision: revision,
			CredentialVersion: "fixture-v1", Images: images, RootPool: stoppedPairPool, DatabasePool: stoppedPairPool}
		// These processes contain no credentials. The fixture-only verifier
		// models revocation, and readiness means only a PID 1 handshake.
		verifier := fixtureGatewayDelivery(func(ctx context.Context, b gateway.NativeGatewayRuntimeBinding) error {
			if b != runtime {
				return errors.New("fixture runtime binding changed")
			}
			return ctx.Err()
		})
		pgReady := false
		ready := fixtureGatewayReadiness(func(ctx context.Context, id gateway.NativeGatewayResourceIdentity, role string) error {
			if id != identity {
				return errors.New("fixture readiness target changed")
			}
			if role == "postgres" {
				if err := execOCI(ctx, sdk, id.PostgresInstance, []string{"/stop-fixture", "started"}); err != nil {
					return err
				}
				pgReady = true
				return nil
			}
			return execOCI(ctx, sdk, id.LiteLLMInstance, []string{"/stop-fixture", "started"})
		})
		client := &stopRetentionSDK{InstanceServer: sdk, beforeSubmit: func(name string, put api.InstanceStatePut, etag string) {
			if put.Action != "start" || put.Force || etag == "" || (name == pair.LiteLLM.Name && !pgReady) {
				t.Fatal("unauthorized or out-of-order fixture start")
			}
		}}
		start := func() (gateway.NativeEnvironmentGatewayStartResult, error) {
			return gateway.StartNativeEnvironmentGatewayWithClient(ctx, desc, binding, top, devices, pair, runtime, client, context.Context.Err, verifier, ready)
		}
		got, err := start()
		if err != nil || !slices.Equal(got.Started, want) || !slices.Equal(got.Ready, want) || !slices.Equal(client.submissions, want) {
			t.Fatalf("guarded fixture startup: %+v: %v", got, err)
		}
		got, err = start()
		if err != nil || len(got.Started) != 0 || !slices.Equal(got.Ready, want) || !slices.Equal(client.submissions, want) {
			t.Fatalf("repeat fixture startup submitted a write: %+v: %v", got, err)
		}
		return top, devices, pair
	}
	siblingTop, _, sibling := bootPair(t, "123e4567-e89b-42d3-a456-426614174021", "10.94.0.21/24")
	for _, tc := range []struct {
		name, id, egress string
		revoke           bool
	}{
		{"stop-and-repeat", "123e4567-e89b-42d3-a456-426614174020", "10.94.0.20/24", false},
		{"guard-loss-and-resume", "123e4567-e89b-42d3-a456-426614174022", "10.94.0.22/24", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			top, devices, pair := bootPair(t, tc.id, tc.egress)
			verifyStopRetention(t, ctx, sdk, top, devices, pair, siblingTop, sibling, tc.revoke)
		})
	}
	t.Run("stopped-replacement-and-removal", func(t *testing.T) {
		top, devices, pair := bootPair(t, "123e4567-e89b-42d3-a456-426614174023", "10.94.0.23/24")
		pg, llm := pair.Postgres.Name, pair.LiteLLM.Name
		const marker = "fixture retained across replacement and removal\n"
		if err := execOCI(ctx, sdk, pg, []string{"/stop-fixture", "seed", devices.Postgres["database"]["path"] + "/replacement-marker", marker}); err != nil {
			t.Fatal("could not write disposable database marker")
		}
		stop := &stopRetentionSDK{InstanceServer: sdk, beforeSubmit: func(string, api.InstanceStatePut, string) {}}
		if _, err := gateway.StopNativeGatewayWithClient(ctx, top, devices, pair, stop, context.Context.Err); err != nil {
			t.Fatal(err)
		}
		oldPG, pgETag := readStoppedPairGuest(t, sdk, pg, top.EnvironmentID, images.Postgres.Expected.IncusFingerprint)
		oldLLM, _ := readStoppedPairGuest(t, sdk, llm, top.EnvironmentID, images.LiteLLM.Expected.IncusFingerprint)
		desc := environment.Descriptor{ID: top.EnvironmentID, Name: "removal-fixture", Binding: binding}
		candidate := environment.Candidate{APIVersion: environment.CandidateAPIVersion, Kind: environment.CandidateKind, Environment: desc,
			SelectedWorkloads: []string{}, Workloads: []acceptedstate.WorkloadSpec{}}
		old, err := environment.BuildAcceptedRevision(nil, nil, candidate)
		if err != nil {
			t.Fatal(err)
		}
		successor := candidate
		successor.Environment.Name = "recreation-fixture"
		next, err := environment.BuildAcceptedRevision(&old, &candidate, successor)
		if err != nil {
			t.Fatal(err)
		}
		replacement := importGatewayFixtureImage(t, ctx, sdk, binding.Project, "litellm-next", executable)
		client := &gatewayRemovalVMClient{stoppedPairSDK: stoppedPairSDK{InstanceServer: sdk}}
		target := gateway.NativeGatewayRemoval{Revision: old, Instance: llm, InstanceCreatedAt: oldLLM.CreatedAt, InstanceUUID: oldLLM.Config["volatile.uuid"]}
		var effects []native.Action
		got, err := gateway.RecreateNativeEnvironmentGatewayLiteLLMWithClient(ctx, desc, binding, top, devices, pair, images, target, next, replacement, client,
			func(ctx context.Context, from, to environment.AcceptedRevision, action native.Action, name string) error {
				if from != old || to != next || name != llm {
					return errors.New("unexpected fixture replacement authorization")
				}
				effects = append(effects, action)
				return ctx.Err()
			})
		if err != nil || !got.Removal.Absent || !got.Removal.DatabaseVolumeRetained ||
			!slices.Equal(got.Creation.Created, []string{llm}) || !slices.Equal(effects, []native.Action{native.Delete, native.Create}) {
			t.Fatalf("guarded stopped replacement: %+v: %v", got, err)
		}
		currentPG, currentETag := readStoppedPairGuest(t, sdk, pg, top.EnvironmentID, images.Postgres.Expected.IncusFingerprint)
		if pgETag != currentETag || !reflect.DeepEqual(currentPG, oldPG) {
			t.Fatal("LiteLLM replacement changed retained PostgreSQL guest")
		}
		newLLM, _ := readStoppedPairGuest(t, sdk, llm, top.EnvironmentID, replacement.Expected.IncusFingerprint)
		if newLLM.Config["volatile.uuid"] == "" || newLLM.Config["volatile.uuid"] == oldLLM.Config["volatile.uuid"] {
			t.Fatal("replacement did not establish a new guest incarnation")
		}
		newTop := top
		newTop.Services = slices.Clone(top.Services)
		newTop.Services[1].ImageReference = replacement.Expected.Registry + "/" + replacement.Expected.Repository + "@" + replacement.Expected.ManifestDigest
		newPair, err := gateway.PlanNativeGatewayInstances(newTop, devices, pair.Postgres.ImageAlias, replacement.Evidence.Alias)
		if err != nil {
			t.Fatal(err)
		}
		newImages := images
		newImages.LiteLLM = replacement
		for _, guest := range []*api.Instance{newLLM, oldPG} {
			target := gateway.NativeGatewayRemoval{Revision: next, Instance: guest.Name, InstanceCreatedAt: guest.CreatedAt, InstanceUUID: guest.Config["volatile.uuid"]}
			out, err := gateway.RemoveNativeEnvironmentGatewayServiceWithClient(ctx, desc, binding, newTop, devices, newPair, newImages, target, client,
				func(ctx context.Context, got gateway.NativeGatewayRemoval) error {
					if got != target {
						return errors.New("unexpected fixture removal authorization")
					}
					return ctx.Err()
				})
			if err != nil || !out.Absent || !out.DatabaseVolumeRetained {
				t.Fatalf("explicit removal: %+v: %v", out, err)
			}
			assertStoppedPairGuestAbsent(t, sdk, guest.Name)
		}
		if !slices.Equal(client.deletions, []string{llm, llm, pg}) || !slices.Equal(client.instanceWrites, []string{llm}) {
			t.Fatal("unexpected disposable destructive submissions")
		}
		body, _, err := sdk.GetStorageVolumeFile(stoppedPairPool, "custom", top.DatabaseVolume, "/replacement-marker")
		assertFixtureBytes(t, "retained database marker after both removals", marker, body, err)
	})
}

type gatewayRemovalVMClient struct {
	stoppedPairSDK
	deletions []string
}

func (c *gatewayRemovalVMClient) DeleteInstance(name string) (native.InstanceOperation, error) {
	c.deletions = append(c.deletions, name)
	return c.InstanceServer.DeleteInstance(name)
}

type fixtureGatewayDelivery func(context.Context, gateway.NativeGatewayRuntimeBinding) error

func (f fixtureGatewayDelivery) VerifyGatewayDelivery(ctx context.Context, b gateway.NativeGatewayRuntimeBinding) error {
	return f(ctx, b)
}

type fixtureGatewayReadiness func(context.Context, gateway.NativeGatewayResourceIdentity, string) error

func (f fixtureGatewayReadiness) VerifyGatewayServiceReady(ctx context.Context, id gateway.NativeGatewayResourceIdentity, role string) error {
	return f(ctx, id, role)
}

func verifyStopRetention(t *testing.T, ctx context.Context, sdk incus.InstanceServer, top gateway.NativeGatewayTopology, devices gateway.NativeGatewayDevices, pair gateway.NativeGatewayInstancePair, siblingTop gateway.NativeGatewayTopology, sibling gateway.NativeGatewayInstancePair, revoke bool) {
	t.Helper()
	pg, llm := pair.Postgres.Name, pair.LiteLLM.Name
	const marker = "aginctus-stop-retention-marker\n"
	if err := execOCI(ctx, sdk, pg, []string{"/stop-fixture", "seed", devices.Postgres["database"]["path"] + "/retention-marker", marker}); err != nil {
		t.Fatalf("write fixture marker through PostgreSQL mount: %v", err)
	}
	readMarker := func() {
		t.Helper()
		body, _, err := sdk.GetStorageVolumeFile(stoppedPairPool, "custom", top.DatabaseVolume, "/retention-marker")
		assertFixtureBytes(t, "custom volume marker", marker, body, err)
	}

	type instanceRecord struct {
		instance *api.Instance
		etag     string
	}
	readInstance := func(name string, status api.StatusCode) instanceRecord {
		t.Helper()
		i, etag, err := sdk.GetInstance(name)
		if err != nil || i.Name != name || i.Project != top.Project || i.StatusCode != status || etag == "" {
			t.Fatalf("expected %s %s: %+v: %v", name, status, i, err)
		}
		return instanceRecord{i, etag}
	}
	type volumeRecord struct {
		volume *api.StorageVolume
		etag   string
	}
	readVolume := func(top gateway.NativeGatewayTopology) volumeRecord {
		t.Helper()
		v, etag, err := sdk.GetStoragePoolVolume(stoppedPairPool, "custom", top.DatabaseVolume)
		if err != nil || v.Name != top.DatabaseVolume || v.Project != top.Project || v.Type != "custom" || v.ContentType != "filesystem" || v.Config["user.aginctus.environment-id"] != top.EnvironmentID || etag == "" {
			t.Fatalf("expected retained custom filesystem volume: %+v: %v", v, err)
		}
		return volumeRecord{v, etag}
	}
	beforePG, beforeLLM := readInstance(pg, api.Running), readInstance(llm, api.Running)
	siblingPG, siblingLLM := readInstance(sibling.Postgres.Name, api.Running), readInstance(sibling.LiteLLM.Name, api.Running)
	volume, siblingVolume := readVolume(top), readVolume(siblingTop)
	assertRetained := func(pgStatus api.StatusCode) (instanceRecord, instanceRecord) {
		t.Helper()
		gotPG, gotLLM := readInstance(pg, pgStatus), readInstance(llm, api.Stopped)
		assertStopFixtureIdentity(t, beforePG.instance, gotPG.instance)
		assertStopFixtureIdentity(t, beforeLLM.instance, gotLLM.instance)
		if !reflect.DeepEqual(volume, readVolume(top)) || !reflect.DeepEqual(siblingVolume, readVolume(siblingTop)) ||
			!reflect.DeepEqual(siblingPG, readInstance(sibling.Postgres.Name, api.Running)) ||
			!reflect.DeepEqual(siblingLLM, readInstance(sibling.LiteLLM.Name, api.Running)) {
			t.Fatal("stop changed a retained volume or the running sibling pair")
		}
		return gotPG, gotLLM
	}
	// Observe daemon state at each actual transition submission, independently
	// of the executor's ordered Changed and Verified output.
	client := &stopRetentionSDK{InstanceServer: sdk, beforeSubmit: func(name string, put api.InstanceStatePut, etag string) {
		if put.Action != "stop" || put.Force || put.Stateful || etag == "" {
			t.Fatalf("unexpected stop request for %s: %+v", name, put)
		}
		readInstance(pg, api.Running)
		switch name {
		case llm:
			readInstance(llm, api.Running)
		case pg:
			readInstance(llm, api.Stopped)
		default:
			t.Fatalf("unexpected stop target %s", name)
		}
	}}
	stop := func(guard func(context.Context) error) (gateway.NativeGatewayStopResult, error) {
		return gateway.StopNativeGatewayWithClient(ctx, top, devices, pair, client, guard)
	}
	wantOrder := []string{llm, pg}
	wantChanged, wantGuards := wantOrder, 2
	guards := 0
	var partialLLM instanceRecord
	if revoke {
		revoked := errors.New("fixture writer grant revoked")
		partial, err := stop(func(ctx context.Context) error {
			guards++
			if len(client.submissions) != 0 {
				return revoked
			}
			return ctx.Err()
		})
		if !errors.Is(err, revoked) || partial.Project != top.Project || !slices.Equal(partial.Changed, []string{llm}) ||
			!slices.Equal(partial.Verified, []string{llm}) || !slices.Equal(client.submissions, []string{llm}) || guards != 2 {
			t.Fatalf("guard loss failed to preserve partial progress: %+v: %v; submissions=%v guards=%d", partial, err, client.submissions, guards)
		}
		var partialPG instanceRecord
		partialPG, partialLLM = assertRetained(api.Running)
		if !reflect.DeepEqual(beforePG, partialPG) {
			t.Fatal("PostgreSQL instance changed despite guard denial")
		}
		wantChanged, wantGuards = []string{pg}, 3
		t.Log("guard loss: LiteLLM stopped; PostgreSQL and sibling pair still running")
	}

	completed, err := stop(func(ctx context.Context) error { guards++; return ctx.Err() })
	if err != nil || completed.Project != top.Project || !slices.Equal(completed.Changed, wantChanged) ||
		!slices.Equal(completed.Verified, wantOrder) || !slices.Equal(client.submissions, wantOrder) || guards != wantGuards {
		t.Fatalf("complete reverse-order stop: %+v: %v; submissions=%v guards=%d", completed, err, client.submissions, guards)
	}
	stoppedPG, stoppedLLM := assertRetained(api.Stopped)
	if revoke && !reflect.DeepEqual(partialLLM, stoppedLLM) {
		t.Fatal("resume changed the already-stopped LiteLLM instance")
	}
	for _, name := range wantOrder {
		body, _, err := sdk.GetInstanceFile(name, "/fixture-stopped")
		assertFixtureBytes(t, name+" shutdown signal receipt", "shutdown signal handled\n", body, err)
	}
	readMarker()
	t.Log("reverse-order stop: both handled shutdown signals; exact marker bytes read directly from retained custom volume")

	repeated, err := stop(func(context.Context) error { t.Fatal("repeat requested writer authority"); return nil })
	if err != nil || repeated.Project != top.Project || len(repeated.Changed) != 0 ||
		!slices.Equal(repeated.Verified, wantOrder) || !slices.Equal(client.submissions, wantOrder) {
		t.Fatalf("repeat was not observation-only: %+v: %v; submissions=%v", repeated, err, client.submissions)
	}
	afterPG, afterLLM := assertRetained(api.Stopped)
	if !reflect.DeepEqual(stoppedPG, afterPG) || !reflect.DeepEqual(stoppedLLM, afterLLM) {
		t.Fatal("repeat changed a stopped instance or its ETag")
	}
	readMarker()
	t.Log("repeat: no new submissions; stopped instances, volume, marker and running sibling unchanged")
}

func assertFixtureBytes(t *testing.T, label, want string, body io.ReadCloser, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("read %s: %v", label, err)
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, int64(len(want)+1)))
	if err != nil || string(data) != want {
		t.Fatalf("%s bytes differ: %q: %v", label, data, err)
	}
}

// Compare persisted intent across a state transition. Incus may legitimately
// change volatile runtime config; keep the base image and instance UUID checks.
func assertStopFixtureIdentity(t *testing.T, before, after *api.Instance) {
	t.Helper()
	stable := func(i *api.Instance) api.InstancePut {
		put := i.Writable()
		put.Config = maps.Clone(put.Config)
		maps.DeleteFunc(put.Config, func(k, _ string) bool {
			return strings.HasPrefix(k, "volatile.") && k != "volatile.base_image" && k != "volatile.uuid"
		})
		return put
	}
	if !before.CreatedAt.Equal(after.CreatedAt) || before.Type != after.Type || !reflect.DeepEqual(stable(before), stable(after)) {
		t.Fatalf("stop changed identity, configuration or attachments of %s", before.Name)
	}
}

type stopRetentionSDK struct {
	incus.InstanceServer
	beforeSubmit func(string, api.InstanceStatePut, string)
	submissions  []string
}

func (c *stopRetentionSDK) UpdateInstanceState(name string, put api.InstanceStatePut, etag string) (native.InstanceOperation, error) {
	c.beforeSubmit(name, put, etag)
	c.submissions = append(c.submissions, name)
	return c.InstanceServer.UpdateInstanceState(name, put, etag)
}
