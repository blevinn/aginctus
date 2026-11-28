// Package nativeoci_test contains disposable feasibility tests, not a production driver.
package nativeoci_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/blevinn/aginctus/internal/gateway"
	agincus "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	"github.com/blevinn/aginctus/internal/orchestration/nativeoci"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
)

// TestDaemonOCI deliberately requires a guest-only marker as well as explicit
// opt-in. Never enable it in the ordinary host test suite.
func TestDaemonOCI(t *testing.T) {
	if os.Getenv("AGINCTUS_NATIVE_OCI_SPIKE") != "disposable-vm" {
		t.Skip("requires the native-oci disposable NixOS VM")
	}
	marker, err := os.ReadFile("/run/aginctus-native-oci-disposable")
	if err != nil || string(marker) != "native-oci-vm\n" {
		t.Fatal("disposable VM marker missing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	server, err := incus.ConnectIncusUnixWithContext(ctx, "/var/lib/incus/unix.socket", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Disconnect()
	status, _, err := server.GetServer()
	if err != nil || status.Config["core.https_address"] != "" {
		t.Fatal("spike requires local-only daemon with no HTTPS listener")
	}
	scope, err := agincus.NewProjectScope("aginctus-native-oci-spike")
	if err != nil {
		t.Fatal(err)
	}
	// Atomic create refuses collisions. Failed tests retain guest state for
	// inspection; destroying the disposable VM is the cleanup boundary.
	for attempt := range 2 {
		result, err := native.EnsureLocalProject(ctx, scope, native.ProjectCreateAuthorization{Confirmed: true})
		if err != nil || !result.Verified || result.Created != (attempt == 0) {
			t.Fatalf("native create/reuse failed: %+v, %v", result, err)
		}
	}
	foreignScope, _ := agincus.NewProjectScope("aginctus-native-foreign")
	if err := server.CreateProject(api.ProjectsPost{Name: foreignScope.Name}); err != nil {
		t.Fatal(err)
	}
	if _, err := native.EnsureLocalProject(ctx, foreignScope, native.ProjectCreateAuthorization{Confirmed: true}); err == nil {
		t.Fatal("native project provisioning adopted foreign object")
	}
	client := server.UseProject(scope.Name)

	// Exercise project-bound, create-or-verify native gateway database storage.
	// These are synthetic VM-only resources, never visible to the pilot daemon.
	// Use the managed volume for the database recreation/persistence proof below.
	descriptor := gatewayVolumeVMEnvironment(scope.Name, status.Environment.CertificateFingerprint, "123e4567-e89b-42d3-a456-426614174000")
	gatewayVolume, err := gateway.EnsureNativeEnvironmentGatewayVolumeWithClient(ctx, descriptor, descriptor.Binding, "spike", client,
		func(context.Context) error { return nil }) // This disposable test is the sole writer.
	if err != nil || !gatewayVolume.Verified || !gatewayVolume.Created {
		t.Fatalf("native gateway volume: %+v %v", gatewayVolume, err)
	}
	if err := server.UseProject("default").CreateNetwork(api.NetworksPost{Name: "spike-mgmt", Type: "bridge", NetworkPut: api.NetworkPut{Config: map[string]string{
		"ipv4.address": "10.78.0.1/24", "ipv4.nat": "false", "ipv4.routing": "false", "ipv4.dhcp.gateway": "none", "ipv6.address": "none",
	}}}); err != nil {
		t.Fatal(err)
	}
	// Static OCI NIC configuration must work without a DHCP server. This
	// bridge is guest-local and unrouted; it grants no external connectivity.
	if err := server.UseProject("default").CreateNetwork(api.NetworksPost{Name: "spike-egress", Type: "bridge", NetworkPut: api.NetworkPut{Config: map[string]string{
		"ipv4.address": "10.79.0.1/24", "ipv4.dhcp": "false", "ipv4.nat": "false", "ipv4.routing": "false", "ipv6.address": "none",
	}}}); err != nil {
		t.Fatal(err)
	}
	otherScope, _ := agincus.NewProjectScope("aginctus-native-image-collision")
	if _, err := native.EnsureLocalProject(ctx, otherScope, native.ProjectCreateAuthorization{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	pinnedReferences := make(map[string]string)
	createImages := make(map[string]gateway.NativeGatewayCreateImage)
	resolvedImages := make(map[string]nativeoci.ResolvedImage)
	for _, image := range []struct{ name, registry, repository, reference string }{
		{"postgres", "docker.io", "library/postgres", "17-alpine"},
		{"litellm", "ghcr.io", "berriai/litellm", "v1.103.0"},
	} {
		t.Run(image.name, func(t *testing.T) {
			wait := func(op incus.Operation, err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
				if err := op.WaitContext(ctx); err != nil {
					t.Fatal(err)
				}
			}
			resolved, err := nativeoci.ResolvePublic(ctx, nativeoci.Source{
				Registry: image.registry, Repository: image.repository, Reference: image.reference,
				Platform: ocispec.Platform{OS: "linux", Architecture: runtime.GOARCH},
			})
			if err != nil {
				t.Fatal(err)
			}
			resolvedImages[image.name] = resolved
			var pgFiles postgresFileFixture
			if image.name == "postgres" {
				pgFiles = newPostgresFileFixture(t, resolved.Config)
			}

			// The create-only gateway acquisition must obtain immutable OCI
			// identity directly through the SDK. Layer-only matches are not
			// sufficient for subsequent adoption/reuse.
			identity := native.GatewayOCIIdentity{
				Registry: resolved.Registry, Repository: resolved.Repository,
				ManifestDigest: resolved.ManifestDigest, ConfigDigest: resolved.ConfigDigest,
				IncusFingerprint: resolved.IncusFingerprint,
			}
			pinnedReferences[image.name] = resolved.Registry + "/" + resolved.Repository + "@" + resolved.ManifestDigest
			acquired, err := native.AcquireLocalGatewayImage(ctx, native.GatewayImageAcquireIntent{
				Project: scope.Name, Image: identity,
			})
			if err != nil || !acquired.Attempted || !acquired.Verified {
				t.Fatalf("guarded native image acquisition: %+v %v", acquired, err)
			}
			if _, err := native.AcquireLocalGatewayImage(ctx, native.GatewayImageAcquireIntent{
				Project: scope.Name, Image: identity,
			}); err == nil {
				t.Fatal("native image acquisition adopted an existing layer fingerprint")
			}
			// A different project's cached image can have the same layers but a
			// different OCI config. It must be rejected before requesting a pull.
			rejected, err := native.AcquireLocalGatewayImage(ctx, native.GatewayImageAcquireIntent{
				Project: otherScope.Name, Image: identity,
			})
			var acquireErr *native.GatewayImageAcquireError
			if !errors.As(err, &acquireErr) || acquireErr.Stage != "preflight" || rejected.Attempted {
				t.Fatalf("cross-project cached image not refused before mutation: %+v %v", rejected, err)
			}
			if _, _, err := server.UseProject(otherScope.Name).GetImage(identity.IncusFingerprint); !api.StatusErrorCheck(err, 404) {
				t.Fatal("cross-project refusal mutated selected image inventory")
			}
			request := api.ImagesPost{Source: &api.ImagesPostSource{
				Type: "image", Mode: "pull", ImageSource: api.ImageSource{
					Protocol: "oci", Server: "https://" + resolved.Registry, Alias: resolved.Repository + "@" + resolved.ManifestDigest,
					ImageType: "container",
				},
			}}
			op, err := client.CreateImage(request, nil)
			wait(op, err)
			fingerprint, _ := op.Get().Metadata["fingerprint"].(string)
			if fingerprint != resolved.IncusFingerprint {
				t.Fatal("daemon OCI fingerprint differs from resolved immutable identity")
			}
			images, err := client.GetImages()
			if err != nil {
				t.Fatal(err)
			}
			before := len(images)
			repeated, err := client.CreateImage(request, nil)
			wait(repeated, err)
			if repeated.Get().Metadata["fingerprint"] != fingerprint {
				t.Fatal("repeated pull changed image fingerprint")
			}
			images, err = client.GetImages()
			if err != nil || len(images) != before {
				t.Fatal("repeated pull changed image inventory")
			}
			name := image.name + "-spike"
			alias := name + "-image"
			// A legitimate acquisition result must not be reused for a different
			// OCI config merely because its Incus layer fingerprint still matches.
			mismatched := native.GatewayAliasCreateIntent{
				Project: scope.Name, Alias: alias, Image: identity, Acquisition: acquired,
			}
			mismatched.Image.ConfigDigest = "sha256:" + strings.Repeat("d", 64)
			rejectedAlias, err := native.CreateLocalGatewayImageAlias(ctx, mismatched)
			if err == nil || rejectedAlias.Created || rejectedAlias.Verified {
				t.Fatalf("alias accepted mismatched acquisition identity: %+v %v", rejectedAlias, err)
			}
			if _, _, err := client.GetImageAlias(alias); !api.StatusErrorCheck(err, 404) {
				t.Fatal("mismatched acquisition mutated alias inventory")
			}
			// The image alias is created only after the same selected-project
			// acquisition has returned a verified result. This disposable VM
			// is single-writer; production requires independently fenced writes.
			createdAlias, err := native.CreateLocalGatewayImageAlias(ctx, native.GatewayAliasCreateIntent{
				Project: scope.Name, Alias: alias, Image: identity, Acquisition: acquired,
			})
			if err != nil || !createdAlias.Created || !createdAlias.Verified {
				t.Fatalf("guarded alias create: %+v %v", createdAlias, err)
			}
			if _, err := native.CreateLocalGatewayImageAlias(ctx, native.GatewayAliasCreateIntent{
				Project: scope.Name, Alias: alias, Image: identity, Acquisition: acquired,
			}); err == nil {
				t.Fatal("native alias create adopted a preexisting alias")
			}
			evidence := native.GatewayImageEvidence{Project: scope.Name, Alias: alias, Identity: identity}
			createImages[image.name] = gateway.NativeGatewayCreateImage{Expected: identity, Evidence: evidence}
			if err := native.ObserveLocalGatewayImageReuse(ctx, native.GatewayImageAcquireIntent{
				Project: scope.Name, Image: identity,
			}, alias, evidence); err != nil {
				t.Fatalf("observed OCI alias identity mismatch: %v", err)
			}
			foreignEvidence := evidence
			foreignEvidence.Identity.ConfigDigest = "sha256:" + strings.Repeat("d", 64)
			if err := native.ObserveLocalGatewayImageReuse(ctx, native.GatewayImageAcquireIntent{
				Project: scope.Name, Image: identity,
			}, alias, foreignEvidence); err == nil {
				t.Fatal("config-only image identity collision accepted")
			}
			config := map[string]string{
				"environment.AGINCTUS_SPIKE": "synthetic", "user.aginctus.managed": "true",
				"user.aginctus.resource": "workload", "user.aginctus.role": "agent-workload", "user.aginctus.workload": image.name,
			}
			if image.name == "postgres" {
				config["environment.POSTGRES_USER"] = "litellm"
				config["environment.POSTGRES_DB"] = "litellm"
				config["environment.POSTGRES_PASSWORD_FILE"] = postgresFixturePasswordPath
				config["environment.POSTGRES_INITDB_ARGS"] = "--auth-host=scram-sha-256"
			}
			devices := map[string]map[string]string{
				"root": {"type": "disk", "path": "/", "pool": "spike"},
				"eth0": {"type": "nic", "name": "eth0", "network": "spike-mgmt", "ipv4.gateway": "none"},
			}
			if image.name == "postgres" {
				devices["data"] = map[string]string{"type": "disk", "pool": "spike", "source": gatewayVolume.Name, "path": "/var/lib/postgresql/data"}
			} else {
				// Exercise the actual device planner rather than a hand-written
				// approximation. Stripping /24 would request DHCP and Incus would
				// reject this NIC on the DHCP-disabled bridge above.
				planned, err := gateway.PlanNativeGatewayDevices(gateway.NativeGatewayTopology{
					Deployment: "spike", Project: scope.Name, ManagementNetwork: "spike-mgmt",
					EgressNetwork: "spike-egress", DatabaseVolume: gatewayVolume.Name, EnvironmentID: descriptor.ID,
					Services: []gateway.NativeGatewayService{
						{Name: "postgres", ImageReference: pinnedReferences["postgres"],
							Networks: []gateway.NativeGatewayNetwork{{Name: "spike-mgmt"}}, Volume: gatewayVolume.Name,
							SecretEnvironment: []string{"AGINCTUS_GATEWAY_POSTGRES_PASSWORD"}},
						{Name: "litellm", ImageReference: pinnedReferences["litellm"],
							Networks:          []gateway.NativeGatewayNetwork{{Name: "spike-mgmt"}, {Name: "spike-egress", IPv4Address: "10.79.0.12/24"}},
							DependsOn:         []string{"postgres"},
							SecretEnvironment: []string{"AGINCTUS_GATEWAY_POSTGRES_PASSWORD", "AGINCTUS_GATEWAY_MASTER_KEY", "AGINCTUS_GATEWAY_SALT_KEY"}},
					},
				}, "spike", "spike")
				if err != nil {
					t.Fatal(err)
				}
				devices = planned.LiteLLM
			}
			instance := api.InstancesPost{
				Name: name, Type: api.InstanceTypeContainer,
				Source:      api.InstanceSource{Type: "image", Fingerprint: fingerprint},
				InstancePut: api.InstancePut{Profiles: []string{}, Config: config, Devices: devices},
			}
			wait(client.CreateInstance(instance))
			observed, _, err := client.GetInstance(name)
			if err != nil || observed.Config["volatile.base_image"] != fingerprint || observed.Config["volatile.container.oci"] != "true" {
				t.Fatal("OCI instance initialization or fingerprint mismatch")
			}
			if image.name == "postgres" {
				pgFiles.stage(t, client, name, false)
			}
			wait(client.UpdateInstanceState(name, api.InstanceStatePut{Action: "start", Timeout: 60}, ""))
			state, _, err := client.GetInstanceState(name)
			if err != nil || state.StatusCode != api.Running {
				t.Fatal("OCI service did not remain running")
			}
			if image.name == "litellm" {
				staticAddress := false
				for _, address := range state.Network["eth1"].Addresses {
					if address.Family == "inet" && address.Address == "10.79.0.12" && address.Netmask == "24" {
						staticAddress = true
					}
				}
				if !staticAddress {
					t.Fatal("planned static OCI egress address missing from independent Incus state")
				}
			}
			intent := native.InstanceIntent{
				Identity: native.InstanceIdentity{Project: scope.Name, Name: name}, WorkloadID: image.name,
				Type: "container", ImageAlias: alias, EnsureRunning: true, Config: config, Profiles: []string{}, Devices: devices,
			}
			for range 2 {
				plan, err := native.PlanLocalExistingInstance(ctx, intent)
				if err != nil || plan.Action != native.Unchanged || len(plan.Changes) != 0 || plan.WriteAuthorized {
					t.Fatalf("native SDK no-op planning failed: %+v, %v", plan, err)
				}
			}
			foreign := intent
			foreign.WorkloadID = "foreign"
			foreign.Config = map[string]string{"user.aginctus.managed": "true", "user.aginctus.resource": "workload", "user.aginctus.role": "agent-workload", "user.aginctus.workload": "foreign"}
			if _, err := native.PlanLocalExistingInstance(ctx, foreign); err == nil {
				t.Fatal("native planner accepted foreign workload identity")
			}
			probe := []string{"python", "-c", "import urllib.request; assert urllib.request.urlopen('http://127.0.0.1:4000/health/liveliness', timeout=5).status == 200"}
			if image.name == "postgres" {
				probe = postgresFixtureQuery(postgresFixturePassPath, "SELECT 1")
			}
			readyCtx, readyCancel := context.WithTimeout(ctx, 2*time.Minute)
			defer readyCancel()
			for {
				if err := execOCI(readyCtx, client, name, probe); err == nil {
					break
				}
				select {
				case <-readyCtx.Done():
					t.Fatal("OCI application readiness timed out")
				case <-time.After(time.Second):
				}
			}
			if image.name == "postgres" {
				pgFiles.verify(t, ctx, client, name, "CREATE TABLE aginctus_spike AS SELECT 42 AS value")
			}
			wait(client.UpdateInstanceState(name, api.InstanceStatePut{Action: "stop", Timeout: 60}, ""))
			wait(client.DeleteInstance(name))
			if image.name == "postgres" {
				if _, _, err := client.GetStoragePoolVolume("spike", "custom", gatewayVolume.Name); err != nil {
					t.Fatal("instance removal lost custom volume")
				}
				wait(client.CreateInstance(instance))
				// A changed initialization file must not rotate a role in an
				// existing database. The old password and marker must still work.
				pgFiles.stage(t, client, name, true)
				wait(client.UpdateInstanceState(name, api.InstanceStatePut{Action: "start", Timeout: 60}, ""))
				retainedCtx, cancelRetained := context.WithTimeout(ctx, 2*time.Minute)
				defer cancelRetained()
				for {
					err := execOCI(retainedCtx, client, name, postgresFixtureQuery(postgresFixturePassPath, postgresFixtureMarkerQuery))
					if err == nil {
						break
					}
					select {
					case <-retainedCtx.Done():
						t.Fatal("database contents not recovered after recreation")
					case <-time.After(time.Second):
					}
				}
				pgFiles.verify(t, ctx, client, name, "SELECT 1")
				pgFiles.rotate(t, ctx, client, name)
				pgFiles.verifyRotated(t, ctx, client, name)
				wait(client.UpdateInstanceState(name, api.InstanceStatePut{Action: "stop", Timeout: 60}, ""))
				wait(client.UpdateInstanceState(name, api.InstanceStatePut{Action: "start", Timeout: 60}, ""))
				pgFiles.verifyRotated(t, ctx, client, name)
				t.Log("PostgreSQL role rotated over SCRAM; data retained and old password rejected before and after restart")
				wait(client.UpdateInstanceState(name, api.InstanceStatePut{Action: "stop", Timeout: 60}, ""))
				wait(client.DeleteInstance(name))
			}
		})
	}
	// The OCI fixture has now proved service readiness and durable data
	// recovery separately. Next prove the new create-only gateway executor
	// leaves two managed containers STOPPED without injecting credentials.
	for _, bridge := range []struct {
		name, resource, cidr, nat, route string
	}{
		{"gw-create-mgmt", "management-network", "10.91.0.1/24", "false", "false"},
		{"gw-egress", "gateway-egress-network", "10.92.0.1/24", "true", "true"},
	} {
		if err := server.UseProject("default").CreateNetwork(api.NetworksPost{
			Name: bridge.name, Type: "bridge", NetworkPut: api.NetworkPut{Config: map[string]string{
				"ipv4.address": bridge.cidr, "ipv4.nat": bridge.nat, "ipv4.routing": bridge.route,
				"ipv6.address": "none", "user.aginctus.managed": "true",
				"user.aginctus.resource": bridge.resource,
			}},
		}); err != nil {
			t.Fatalf("gateway create fixture bridge: %v", err)
		}
	}
	topology := gateway.NativeGatewayTopology{
		Deployment: "spike", Project: scope.Name, ManagementNetwork: "gw-create-mgmt",
		EgressNetwork: "gw-egress", DatabaseVolume: "gateway-postgres",
		Services: []gateway.NativeGatewayService{
			{Name: "postgres", ImageReference: pinnedReferences["postgres"],
				Networks:          []gateway.NativeGatewayNetwork{{Name: "gw-create-mgmt"}},
				Volume:            "gateway-postgres",
				SecretEnvironment: []string{"AGINCTUS_GATEWAY_POSTGRES_PASSWORD"}},
			{Name: "litellm", ImageReference: pinnedReferences["litellm"],
				Networks: []gateway.NativeGatewayNetwork{
					{Name: "gw-create-mgmt"},
					{Name: "gw-egress", IPv4Address: "10.92.0.12/24"},
				}, DependsOn: []string{"postgres"},
				SecretEnvironment: []string{"AGINCTUS_GATEWAY_POSTGRES_PASSWORD", "AGINCTUS_GATEWAY_MASTER_KEY", "AGINCTUS_GATEWAY_SALT_KEY"}},
		},
	}
	topology, err = gateway.BindNativeGatewayTopology(topology, descriptor, descriptor.Binding)
	if err != nil {
		t.Fatal(err)
	}
	gatewayDevices, err := gateway.PlanNativeGatewayDevices(topology, "spike", "spike")
	if err != nil {
		t.Fatal(err)
	}
	gatewayPair, err := gateway.PlanNativeGatewayInstances(topology, gatewayDevices, "postgres-spike-image", "litellm-spike-image")
	if err != nil {
		t.Fatal(err)
	}
	fenceChecks := 0
	fence := func(context.Context) error { fenceChecks++; return nil }
	gatewayImages := gateway.NativeGatewayCreateImages{Postgres: createImages["postgres"], LiteLLM: createImages["litellm"]}
	created, err := gateway.CreateStoppedGatewayWithClient(ctx, topology, gatewayDevices, gatewayPair, gatewayImages, gatewayStoppedVMClient{client}, fence)
	if err != nil || len(created.Created) != 2 || len(created.Verified) != 2 || fenceChecks != 2 {
		t.Fatalf("real Incus stopped native gateway create: %+v %v guards=%d", created, err, fenceChecks)
	}
	for _, name := range []string{gatewayPair.Postgres.Name, gatewayPair.LiteLLM.Name} {
		got, _, err := client.GetInstance(name)
		if err != nil || got.Status != "Stopped" || got.Config["user.aginctus.resource"] != "gateway-instance" {
			t.Fatalf("native gateway instance not owned and stopped: %s %v", name, err)
		}
		if _, found := got.Config["environment.POSTGRES_PASSWORD"]; found {
			t.Fatal("native create leaked runtime credentials")
		}
	}
	repeat, err := gateway.CreateStoppedGatewayWithClient(ctx, topology, gatewayDevices, gatewayPair, gatewayImages, gatewayStoppedVMClient{client}, fence)
	if err != nil || len(repeat.Created) != 0 || len(repeat.Verified) != 2 || fenceChecks != 2 {
		t.Fatalf("native stopped gateway reuse was not a no-op: %+v %v", repeat, err)
	}
	// Exercise the real fixed-file publisher on the disposable stopped pair.
	// This scenario owns the whole daemon and is the sole writer. The receipt
	// proves readback, not persistence across startup or delegated authority.
	token := func() string {
		var b [32]byte
		if _, err := rand.Read(b[:]); err != nil {
			t.Fatal("synthetic token generation failed")
		}
		return hex.EncodeToString(b[:])
	}
	password, master, salt := token(), token(), token()
	assertRedacted := func(observation any) {
		t.Helper()
		data, err := json.Marshal(observation)
		if err != nil {
			t.Fatal("could not inspect gateway evidence")
		}
		for _, secret := range []string{password, master, salt} {
			if strings.Contains(string(data), secret) {
				t.Fatal("gateway evidence contained credential material")
			}
		}
	}
	material, err := gateway.NewGatewayCredentialMaterial("synthetic-v1", password, master, salt)
	if err != nil {
		t.Fatal("synthetic material rejected")
	}
	candidate := environment.Candidate{
		APIVersion: environment.CandidateAPIVersion, Kind: environment.CandidateKind,
		Environment: descriptor, SelectedWorkloads: []string{},
	}
	// A complete typed candidate is necessary even for this disposable pin.
	candidate.Workloads = []acceptedstate.WorkloadSpec{}
	gatewayImage := func(role string) environment.GatewayImageSpec {
		resolved := resolvedImages[role]
		return environment.GatewayImageSpec{Registry: resolved.Registry, Repository: resolved.Repository,
			Architecture: runtime.GOARCH, ManifestDigest: resolved.ManifestDigest, ConfigDigest: resolved.ConfigDigest,
			IncusFingerprint: resolved.IncusFingerprint, Alias: createImages[role].Evidence.Alias}
	}
	candidate.Gateway = &environment.GatewaySpec{
		ManagementNetwork: topology.ManagementNetwork, EgressNetwork: topology.EgressNetwork,
		EgressIPv4: "10.92.0.12/24", RootPool: "spike", DatabasePool: "spike",
		Postgres: gatewayImage("postgres"), LiteLLM: gatewayImage("litellm"), CredentialVersion: "synthetic-v1",
	}
	revision, err := environment.BuildAcceptedRevision(nil, nil, candidate)
	if err != nil {
		t.Fatal("synthetic gateway revision construction failed")
	}
	soleWriter := func(ctx context.Context) error { return ctx.Err() }
	stage := func(guard func(context.Context) error) (gateway.GatewayCredentialDeliveryReceipt, error) {
		return gateway.StageStoppedGatewayCredentialFiles(ctx, revision, candidate, topology, gatewayDevices, gatewayPair,
			gatewayImages, resolvedImages["postgres"], resolvedImages["litellm"], material, client, guard)
	}
	if _, err := stage(nil); err == nil {
		t.Fatal("publisher accepted missing writer guard")
	} else {
		assertRedacted(err.Error())
	}
	receipt, err := stage(soleWriter)
	if err != nil {
		assertRedacted(err.Error())
		t.Fatalf("stopped gateway credential staging: %v", err) // The publisher reports only fixed diagnostic phases.
	}
	if !receipt.Verified || receipt.Version != "synthetic-v1" {
		t.Fatal("stopped gateway credential readback failed")
	}
	if _, err := stage(soleWriter); err == nil {
		t.Fatal("publisher overwrote an existing staged credential handoff")
	} else {
		assertRedacted(err.Error())
	}
	assertRedacted(receipt)
	for _, guest := range []string{gatewayPair.Postgres.Name, gatewayPair.LiteLLM.Name} {
		instance, _, err := client.GetInstance(guest)
		if err != nil {
			t.Fatal("could not inspect stopped gateway metadata")
		}
		assertRedacted(instance)
	}
	// Read-only native status must recognize both owned stopped service
	// containers while preserving the existing image and volume inventory.
	gatewayStatus, err := gateway.InspectNativeGatewayStatusWithReader(ctx, topology, gatewayDevices, gatewayPair, gatewayStopVMClient{client})
	if err != nil || !gatewayStatus.AllStopped || gatewayStatus.AllRunning || len(gatewayStatus.Services) != 2 ||
		gatewayStatus.Services[0].Name != gatewayPair.Postgres.Name || gatewayStatus.Services[1].Name != gatewayPair.LiteLLM.Name {
		t.Fatalf("native gateway real-Incus status failed: %+v %v", gatewayStatus, err)
	}
	// Stop is tested as a non-destructive no-op on the new stopped service
	// containers. We deliberately do NOT start unconfigured containers merely
	// to test the stop operation in this disposable VM.
	noopGuardCalls := 0
	noopStop, err := gateway.StopNativeGatewayWithClient(ctx, topology, gatewayDevices, gatewayPair,
		gatewayStopVMClient{client}, func(context.Context) error { noopGuardCalls++; return nil })
	if err != nil || len(noopStop.Changed) != 0 || len(noopStop.Verified) != 2 || noopGuardCalls != 0 {
		t.Fatalf("native stopped service noop: %+v %v guards=%d", noopStop, err, noopGuardCalls)
	}
	if _, err := gateway.StopNativeGatewayWithClient(ctx, topology, gatewayDevices, gatewayPair,
		gatewayStopVMClient{client}, nil); err == nil {
		t.Fatal("stop permitted missing authorization guard")
	}
	// Alter the ownership marker through the disposable test administrator
	// only. The guarded stop must refuse the foreign instance and retain all
	// resources. No attempt is made to repair/adopt it.
	foreignInstance, etag, err := client.GetInstance(gatewayPair.LiteLLM.Name)
	if err != nil {
		t.Fatal(err)
	}
	foreignPut := foreignInstance.Writable()
	foreignPut.Config["user.aginctus.resource"] = "foreign"
	op, err := client.UpdateInstance(gatewayPair.LiteLLM.Name, foreignPut, etag)
	if err != nil {
		t.Fatal(err)
	}
	if err := op.WaitContext(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := gateway.StopNativeGatewayWithClient(ctx, topology, gatewayDevices, gatewayPair,
		gatewayStopVMClient{client}, func(context.Context) error { return nil }); err == nil {
		t.Fatal("stop accepted foreign existing gateway ownership")
	}
	if _, err := gateway.InspectNativeGatewayStatusWithReader(ctx, topology, gatewayDevices, gatewayPair, gatewayStopVMClient{client}); err == nil {
		t.Fatal("native status accepted foreign existing gateway ownership")
	}
	if _, _, err := client.GetStoragePoolVolume("spike", "custom", gatewayVolume.Name); err != nil {
		t.Fatal("failed stop must preserve database volume")
	}
	t.Run("authenticated-native-pair",func(t *testing.T){testNativeAuthenticatedPair(t,ctx,server,client,descriptor,createImages,resolvedImages)})
	images, err := server.UseProject("default").GetImages()
	if err != nil || len(images) != 0 {
		t.Fatal("OCI import leaked into the default project")
	}
}

func execOCI(ctx context.Context, client incus.InstanceServer, name string, command []string) error {
	code, err := execOCIStatus(ctx, client, name, command)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("synthetic application probe failed")
	}
	return nil
}

func execOCIStatus(ctx context.Context, client incus.InstanceServer, name string, command []string) (float64, error) {
	return execOCIStatusWithInput(ctx, client, name, command, nil)
}

func execOCIStatusWithInput(ctx context.Context, client incus.InstanceServer, name string, command []string, stdin io.ReadCloser) (float64, error) {
	done := make(chan bool)
	op, err := client.ExecInstance(name, api.InstanceExecPost{Command: command, WaitForWS: true}, &incus.InstanceExecArgs{Stdin: stdin, Stdout: io.Discard, Stderr: io.Discard, DataDone: done})
	if err != nil {
		return 0, err
	}
	if err := op.WaitContext(ctx); err != nil {
		return 0, err
	}
	select {
	case <-done:
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	code, ok := op.Get().Metadata["return"].(float64)
	if !ok {
		return 0, fmt.Errorf("synthetic application exit status unavailable")
	}
	return code, nil
}

// gatewayStoppedVMClient adapts the real project-scoped Incus SDK to the
// create-only interface, without adding a production gateway activation path.
type gatewayStoppedVMClient struct{ incus.InstanceServer }

func (c gatewayStoppedVMClient) CreateInstance(req api.InstancesPost) (native.InstanceCreateOperation, error) {
	return c.InstanceServer.CreateInstance(req)
}

type gatewayStopVMClient struct{ incus.InstanceServer }

func (c gatewayStopVMClient) UpdateInstanceState(name string, state api.InstanceStatePut, etag string) (native.InstanceOperation, error) {
	return c.InstanceServer.UpdateInstanceState(name, state, etag)
}
