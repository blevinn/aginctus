package nativelifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/managementnetwork"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
	check "github.com/blevinn/aginctus/pkg/orchestration"
	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

func TestLocalNativeJoinedWorkloadHerdrVM(t *testing.T) {
	if os.Getenv("AGINCTUS_JOINED_LIFECYCLE_VM") != "disposable-vm" {
		t.Skip("run through the disposable joined lifecycle NixOS VM target")
	}
	marker, err := os.Lstat("/run/aginctus-joined-lifecycle-disposable")
	if err != nil || !marker.Mode().IsRegular() || marker.Mode().Perm()&0o022 != 0 || os.Geteuid() != 1000 {
		t.Fatal("requires disposable guest marker and non-root fixture principal")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	read := func(file string) []byte {
		t.Helper()
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	const project, pool, bridge = "team", "default", "fixture-mgmt"
	pin, image := os.Getenv("AGINCTUS_OBSERVATION_SERVER_PIN"), os.Getenv("AGINCTUS_OBSERVATION_IMAGE")
	enrolled := environment.Descriptor{ID: "c03124df-64c1-49c9-ad0a-32b61547f657", Name: "fixture", Binding: environment.Binding{Project: project, ServerID: "sha256:" + pin}}
	client, err := environment.OpenExplicitRestrictedIncusProject(ctx, environment.ExplicitRestrictedIncusConnection{
		URL: "https://127.0.0.1:8443", Project: project,
		ServerCertificatePEM: string(read("/run/aginctus-observation-server.crt")), TrustedServerSHA256: pin,
		ClientCertificatePEM: string(read("/home/operator/.config/incus/client.crt")), ClientPrivateKeyPEM: string(read("/home/operator/.config/incus/client.key")),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect()
	// The host fixture exports a read-only bridge snapshot. The unprivileged
	// execution principal has only restricted TLS access to the real daemon.
	var network api.Network
	if err := json.Unmarshal(read("/run/aginctus-joined-bridge.json"), &network); err != nil {
		t.Fatal(err)
	}
	bridgeSpec := managementnetwork.Spec{Name: bridge, IPv4Address: "10.79.37.1/24", IPv4DHCPGateway: "none", IPv6Address: "none"}
	verifyProject := func(ctx context.Context) error {
		err := native.VerifyProjectTransport(ctx, client, enrolled.Binding, pin)
		if err != nil {
			t.Logf("fixture transport verification: %v", err)
		}
		return err
	}
	composer := LocalComposer{Environment: enrolled, TrustedServerPin: pin, Client: client, Bridge: ManagementNetwork{
		Spec: bridgeSpec, Reader: bridgeFixture{network}, VerifyBinding: func(ctx context.Context, binding environment.Binding, spec managementnetwork.Spec) error {
			if binding != enrolled.Binding || spec != bridgeSpec || !network.Managed {
				return errors.New("wrong fixture bridge capability")
			}
			return verifyProject(ctx)
		}}, VerifyAttachments: func(ctx context.Context, env environment.Descriptor, instance, storage, nic string) error {
		if !env.SameEnvironment(enrolled) || (instance != "joined-agent" && instance != "joined-herdr") || storage != pool || nic != bridge {
			return errors.New("wrong fixture attachment coordinates")
		}
		p, _, err := client.GetProject(project)
		if err != nil || p == nil || p.Name != project || p.Config["restricted"] != "true" || p.Config["restricted.networks.access"] != bridge || p.Config["restricted.storage-pools.access"] != pool {
			return errors.New("wrong project attachment policy")
		}
		return ctx.Err()
	}}
	candidate := environment.Candidate{APIVersion: environment.CandidateAPIVersion, Kind: environment.CandidateKind, Environment: enrolled,
		SelectedWorkloads: []string{"agent"}, Workloads: []acceptedstate.WorkloadSpec{{ID: "agent", InstanceName: "joined-agent", InstanceType: "container", ImageFingerprint: image, Profiles: []string{}, EnsureRunning: true,
			Native: &acceptedstate.WorkloadNativeSpec{Runtime: "fixture-runtime", StoragePool: pool, ManagementNetwork: bridge}}},
		Herdr: &environment.HerdrSpec{InstanceName: "joined-herdr", ImageFingerprint: image, StoragePool: pool, ManagementNetwork: bridge, AllowStateBootstrap: true},
	}
	store, err := environment.InitializeLocalProjectStateStore(filepath.Join(t.TempDir(), "project"), enrolled.Binding)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := store.PromoteLocalCandidate(ctx, enrolled, nil, candidate, func(ctx context.Context, r environment.LocalPromotionRequest) (environment.Binding, error) {
		if !r.Environment.SameEnvironment(enrolled) || r.CandidateDigest == "" {
			return environment.Binding{}, errors.New("invalid fixture promotion")
		}
		return enrolled.Binding, verifyProject(ctx)
	})
	if err != nil {
		t.Fatal(err)
	}
	exec := orchestration.LocalNativeExecution{Store: store, Environment: enrolled, BootstrapHerdr: true, BindLifecycle: composer.BindLifecycle,
		Authorize: func(ctx context.Context, req orchestration.LocalNativeAuthorityRequest) (environment.Binding, error) {
			if req.Plan.Revision != revision || !req.Environment.SameEnvironment(enrolled) || req.Permission == orchestration.LocalNativeSubmit && (req.Effect.Target != req.Resource.ID || req.Resource.ID == ManagementNetworkID) {
				return environment.Binding{}, errors.New("invalid scoped fixture request")
			}
			return enrolled.Binding, verifyProject(ctx)
		}, CheckPolicy: orchestration.CheckPolicy{Mode: orchestration.ReadOnlyChecks, Authorize: func(ctx context.Context, scope orchestration.TargetScope, c check.Check) error {
			if scope.Deployment != enrolled.ID || c.Effect != check.ReadOnly {
				return errors.New("wrong fixture observation scope")
			}
			return ctx.Err()
		}},
	}
	engine, plan := orchestration.NewEngine(nil), lifecyclePlan(revision, "agent")
	dry, err := exec.ExecuteLocalNative(ctx, engine, plan, orchestration.ExecuteOptions{DryRun: true})
	if err != nil || dry.Native == nil || len(dry.Native.Previews) != 4 || len(dry.Native.Effects) != 0 {
		t.Fatalf("restricted dry run: %+v %v", dry, err)
	}
	for _, name := range []string{"joined-agent", "joined-herdr"} {
		if i, _, err := client.GetInstance(name); i != nil || !api.StatusErrorCheck(err, 404) {
			t.Fatalf("dry run mutated or could not verify absent instance %s: %v", name, err)
		}
	}
	result, err := exec.ExecuteLocalNative(ctx, engine, plan, orchestration.ExecuteOptions{})
	if err != nil || result.Native == nil || len(result.Native.Effects) != 5 || len(result.Conditions) != 4 {
		t.Fatalf("restricted application: %+v %v", result, err)
	}
	journal, err := store.ReadLocalNativeOperation(ctx, enrolled, result.Native.LocalOperationID)
	if err != nil || journal.Status != environment.LocalNativeCompleted || len(journal.Effects) != 5 || journal.Effects[2].Target != herdrclient.NativeVolumeID {
		t.Fatalf("journal evidence: %+v %v", journal, err)
	}
	volume, _, err := client.GetStoragePoolVolume(pool, "custom", "joined-herdr-state-system")
	if err != nil || volume == nil || volume.CreatedAt.IsZero() {
		t.Fatalf("Herdr volume unavailable: %v", err)
	}
	exec.BootstrapHerdr = false
	result, err = exec.ExecuteLocalNative(ctx, engine, plan, orchestration.ExecuteOptions{})
	if err != nil || result.Native == nil || len(result.Native.Effects) != 0 {
		t.Fatalf("restricted idempotence: %+v %v", result, err)
	}
	again, _, err := client.GetStoragePoolVolume(pool, "custom", "joined-herdr-state-system")
	if err != nil || again == nil || !again.CreatedAt.Equal(volume.CreatedAt) {
		t.Fatalf("Herdr state incarnation changed: %v", err)
	}
	const sentinel = "joined retained state\n"
	if err := client.CreateInstanceFile("joined-herdr", "/var/lib/aginctus/state/system/fixture-sentinel", incus.InstanceFileArgs{Content: strings.NewReader(sentinel), UID: 0, GID: 0, Mode: 0600, Type: "file"}); err != nil {
		t.Fatalf("write fixture sentinel through the Herdr guest mount: %v", err)
	}
	plan.Resources = []orchestration.NativeResource{
		{ID: "agent", Kind: orchestration.NativeWorkload},
		{ID: herdrclient.NativeVolumeID, Kind: orchestration.NativeHerdrVolume},
		{ID: herdrclient.NativeInstanceID, Kind: orchestration.NativeHerdrInstance, DependsOn: []string{herdrclient.NativeVolumeID}},
	}
	instanceNames := map[string]string{"agent": "joined-agent", herdrclient.NativeInstanceID: "joined-herdr"}
	live := make(map[string]time.Time, len(instanceNames))
	for id, name := range instanceNames {
		i, _, err := client.GetInstance(name)
		if err != nil || i == nil || i.Status != "Running" || i.Name != name || i.Project != project || i.Config["user.aginctus.environment-id"] != enrolled.ID || i.CreatedAt.IsZero() {
			t.Fatalf("cannot grant teardown on an unverified instance %s: %v", id, err)
		}
		live[id] = i.CreatedAt
	}
	exec.BindLifecycleTeardown = composer.BindTeardown
	selection := orchestration.LocalLifecycleTeardown{Instances: maps.Clone(live), HerdrState: volume.CreatedAt}
	var operation native.ProjectInstanceOperation
	var action native.Action
	exec.AuthorizeTeardown = func(ctx context.Context, req orchestration.LocalLifecycleTeardownRequest) (environment.Binding, error) {
		if req.Plan.Revision != revision || req.Selection.Operation != operation || !maps.EqualFunc(req.Selection.Instances, live, time.Time.Equal) || !req.Selection.HerdrState.Equal(volume.CreatedAt) {
			return environment.Binding{}, errors.New("fixture teardown scope denied")
		}
		if req.Permission == orchestration.LocalNativeSubmit {
			name, ok := instanceNames[req.Effect.Target]
			if !ok || req.Resource.ID != req.Effect.Target || req.Effect.Action != action {
				return environment.Binding{}, errors.New("fixture teardown effect denied")
			}
			i, _, err := client.GetInstance(name)
			if err != nil || i == nil || !i.CreatedAt.Equal(live[req.Effect.Target]) || i.Project != project || i.Name != name {
				return environment.Binding{}, errors.New("fixture teardown incarnation no longer authorized")
			}
		}
		return enrolled.Binding, verifyProject(ctx)
	}
	for _, op := range []native.ProjectInstanceOperation{native.StopInstance, native.RemoveInstance} {
		operation, selection.Operation = op, op
		action = native.Stop
		if op == native.RemoveInstance {
			action = native.Delete
		}
		result, err := exec.ExecuteLocalTeardown(ctx, engine, plan, selection, orchestration.ExecuteOptions{})
		if err != nil || result.Native == nil || len(result.Native.Effects) != 2 || len(result.Conditions) != 3 {
			t.Fatalf("restricted teardown %s: %+v %v", op, result, err)
		}
		journal, err := store.ReadLocalNativeOperation(ctx, enrolled, result.Native.LocalOperationID)
		if err != nil || journal.Status != environment.LocalNativeCompleted || len(journal.Effects) != 2 {
			t.Fatalf("teardown journal: %+v %v", journal, err)
		}
		for _, e := range journal.Effects {
			if e.Action != string(action) || e.Outcome != environment.LocalNativeApplied {
				t.Fatalf("unexpected teardown effect: %+v", e)
			}
		}
		for id, name := range instanceNames {
			i, _, err := client.GetInstance(name)
			if op == native.StopInstance && (err != nil || i == nil || i.Status != "Stopped" || !i.CreatedAt.Equal(live[id])) ||
				op == native.RemoveInstance && (i != nil || !api.StatusErrorCheck(err, http.StatusNotFound)) {
				t.Fatalf("instance %s did not reach expected state for %s: %v", id, op, err)
			}
		}
	}
	retained, _, err := client.GetStoragePoolVolume(pool, "custom", volume.Name)
	if err != nil || retained == nil || !retained.CreatedAt.Equal(volume.CreatedAt) || len(retained.UsedBy) != 0 {
		t.Fatalf("Herdr volume or detachment unverified: %v", err)
	}
	sftp, err := client.GetStoragePoolVolumeFileSFTP(pool, "custom", volume.Name)
	if err != nil {
		t.Fatal(err)
	}
	defer sftp.Close()
	file, err := sftp.Open("/fixture-sentinel")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if data, err := io.ReadAll(io.LimitReader(file, 128)); err != nil || string(data) != sentinel {
		t.Fatalf("Herdr guest data was not retained: %v", err)
	}
}
