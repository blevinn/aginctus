package nativeoci_test

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	"github.com/blevinn/aginctus/internal/orchestration/nativeoci"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
	check "github.com/blevinn/aginctus/pkg/orchestration"
)

const stoppedPairPool = "spike"

func TestNativeGatewayStoppedPairVM(t *testing.T) {
	ctx, admin, binding := openGatewayFixtureVM(t, "aginctus-native-stopped-pair")
	createGatewayFixtureBridges(t, admin)
	sdk := admin.UseProject(binding.Project)
	images := gateway.NativeGatewayCreateImages{
		Postgres: importGatewayFixtureImage(t, ctx, sdk, binding.Project, "postgres", nil),
		LiteLLM:  importGatewayFixtureImage(t, ctx, sdk, binding.Project, "litellm", nil),
	}
	for _, tc := range []struct{ name, id, egress string }{
		{"create-and-repeat", "123e4567-e89b-42d3-a456-426614174010", "10.94.0.10/24"},
		{"guard-loss", "123e4567-e89b-42d3-a456-426614174011", "10.94.0.11/24"},
		{"foreign-collision", "123e4567-e89b-42d3-a456-426614174012", "10.94.0.12/24"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			descriptor := environment.Descriptor{ID: tc.id, Name: "stopped-pair-fixture", Binding: binding}
			base := "ag-" + strings.ReplaceAll(tc.id, "-", "")
			pgName, llmName, volumeName := base+"-pg", base+"-llm", base+"-pgdata"
			top, err := gateway.BindNativeGatewayTopology(stoppedPairCandidate(binding.Project, images, tc.egress), descriptor, binding)
			if err != nil {
				t.Fatal(err)
			}
			client := &stoppedPairSDK{InstanceServer: sdk}
			// The isolated VM has no other writers. This revocable fixture guard
			// models loss of the caller's grant, not production exclusion.
			revoked := errors.New("fixture writer grant revoked")
			held, checks := true, 0
			guard := func(ctx context.Context) error {
				checks++
				if !held {
					return revoked
				}
				return ctx.Err()
			}
			ensureVolume := func(created bool) {
				t.Helper()
				v, err := gateway.EnsureNativeEnvironmentGatewayVolumeWithClient(ctx, descriptor, binding, stoppedPairPool, client, guard)
				if err != nil || !v.Verified || v.Created != created || v.Project != binding.Project || v.EnvironmentID != tc.id || v.Pool != stoppedPairPool || v.Name != volumeName {
					t.Fatalf("volume create/reuse: %+v: %v", v, err)
				}
			}
			devices, err := gateway.PlanNativeGatewayDevices(top, stoppedPairPool, stoppedPairPool)
			if err != nil {
				t.Fatal(err)
			}
			pair, err := gateway.PlanNativeGatewayInstances(top, devices, images.Postgres.Evidence.Alias, images.LiteLLM.Evidence.Alias)
			if err != nil {
				t.Fatal(err)
			}
			if pair.Postgres.Name != pgName || pair.LiteLLM.Name != llmName {
				t.Fatalf("unexpected planned guest names: %s, %s", pair.Postgres.Name, pair.LiteLLM.Name)
			}
			preview, err := gateway.PreviewNativeEnvironmentGatewayStoppedWithReader(ctx, descriptor, binding, top, devices, pair, images, client)
			if err != nil || !preview.CreateDatabaseVolume || len(preview.Instances.Steps) != 2 || len(client.instanceWrites) != 0 || client.volumeWrites != 0 {
				t.Fatalf("initial stopped preview: %+v: %v", preview, err)
			}
			if tc.name != "foreign-collision" {
				ensureVolume(true)
				if checks != 1 || client.volumeWrites != 1 {
					t.Fatalf("volume guard checks=%d submissions=%d", checks, client.volumeWrites)
				}
			}
			create := func() (gateway.NativeGatewayStoppedResult, error) {
				return gateway.CreateStoppedGatewayWithClient(ctx, top, devices, pair, images, client, guard)
			}
			verifyGuests := func(both bool) {
				t.Helper()
				pg, _ := readStoppedPairGuest(t, sdk, pgName, tc.id, images.Postgres.Expected.IncusFingerprint)
				if !maps.EqualFunc(pg.Devices, devices.Postgres, maps.Equal) || pg.Devices["database"]["source"] != volumeName || pg.Devices["database"]["pool"] != stoppedPairPool {
					t.Fatalf("wrong PostgreSQL attachments: %v", pg.Devices)
				}
				if both {
					llm, _ := readStoppedPairGuest(t, sdk, llmName, tc.id, images.LiteLLM.Expected.IncusFingerprint)
					if !maps.EqualFunc(llm.Devices, devices.LiteLLM, maps.Equal) || llm.Devices["database"] != nil || llm.Devices["egress"]["ipv4.address"] != tc.egress {
						t.Fatalf("wrong LiteLLM attachments: %v", llm.Devices)
					}
				}
			}
			readVolume := func() (*api.StorageVolume, string) {
				t.Helper()
				v, etag, err := sdk.GetStoragePoolVolume(stoppedPairPool, "custom", volumeName)
				if err != nil || v.Name != volumeName || v.Project != binding.Project || v.Type != "custom" || v.ContentType != "filesystem" || v.Config["user.aginctus.environment-id"] != tc.id {
					t.Fatalf("unexpected observed database volume: %+v: %v", v, err)
				}
				return v, etag
			}
			switch tc.name {
			case "create-and-repeat":
				want := []string{pgName, llmName}
				got, err := create()
				if err != nil || got.Project != binding.Project || !slices.Equal(got.Created, want) || !slices.Equal(got.Verified, want) || !slices.Equal(client.instanceWrites, want) || checks != 3 {
					t.Fatalf("pair creation: %+v: %v, writes=%v checks=%d", got, errors.Unwrap(err), client.instanceWrites, checks)
				}
				verifyGuests(true)
				before, etag := readVolume()
				ensureVolume(false)
				got, err = create()
				if err != nil || got.Project != binding.Project || len(got.Created) != 0 || !slices.Equal(got.Verified, want) || !slices.Equal(client.instanceWrites, want) || client.volumeWrites != 1 || checks != 3 {
					t.Fatalf("repeat was not observation-only: %+v: %v, writes=%v checks=%d", got, err, client.instanceWrites, checks)
				}
				verifyGuests(true)
				after, afterETag := readVolume()
				if etag != afterETag || !reflect.DeepEqual(before, after) {
					t.Fatal("repeat changed database volume")
				}
			case "guard-loss":
				client.afterSubmit = func() { held = false }
				got, err := create()
				var stoppedErr *gateway.NativeGatewayStoppedError
				want := []string{pgName}
				if !errors.As(err, &stoppedErr) || !errors.Is(err, revoked) || stoppedErr.Stage != "authority" || stoppedErr.Service != llmName || stoppedErr.Attempted || stoppedErr.Uncertain || !slices.Equal(got.Created, want) || !slices.Equal(got.Verified, want) || !slices.Equal(client.instanceWrites, want) || checks != 3 {
					t.Fatalf("guard loss misreported: %+v: %+v (%v), writes=%v checks=%d", got, stoppedErr, errors.Unwrap(err), client.instanceWrites, checks)
				}
				verifyGuests(false)
				readVolume()
				assertStoppedPairGuestAbsent(t, sdk, llmName)
			case "foreign-collision":
				const foreignID = "123e4567-e89b-42d3-a456-426614174099"
				fingerprint := images.LiteLLM.Expected.IncusFingerprint
				waitStoppedPairOp(t, ctx, func() (incus.Operation, error) {
					return sdk.CreateInstance(api.InstancesPost{
						Name: llmName, Type: api.InstanceTypeContainer,
						Source: api.InstanceSource{Type: "image", Fingerprint: fingerprint},
						InstancePut: api.InstancePut{Profiles: []string{}, Devices: devices.LiteLLM, Config: map[string]string{
							"user.aginctus.managed": "true", "user.aginctus.resource": "gateway-instance", "user.aginctus.role": pair.LiteLLM.Role,
							"user.aginctus.deployment": binding.Project, "user.aginctus.environment-id": foreignID,
						}},
					})
				})
				before, etag := readStoppedPairGuest(t, sdk, llmName, foreignID, fingerprint)
				if _, err := gateway.PreviewNativeEnvironmentGatewayStoppedWithReader(ctx, descriptor, binding, top, devices, pair, images, client); err == nil || client.volumeWrites != 0 {
					t.Fatal("preview failed to block foreign collision before volume creation")
				}
				// Seed a volume solely to exercise the low-level stopped primitive's
				// independent refusal of the same collision after attachments exist.
				ensureVolume(true)
				volume, volumeETag := readVolume()
				got, err := create()
				var stoppedErr *gateway.NativeGatewayStoppedError
				if !errors.As(err, &stoppedErr) || stoppedErr.Stage != "instances" || stoppedErr.Service != "gateway" || stoppedErr.Attempted || stoppedErr.Uncertain || len(got.Created) != 0 || len(got.Verified) != 0 || len(client.instanceWrites) != 0 || checks != 1 {
					t.Fatalf("foreign guest not rejected before writing: %+v: %+v (%v), writes=%v checks=%d", got, stoppedErr, errors.Unwrap(err), client.instanceWrites, checks)
				}
				after, afterETag := readStoppedPairGuest(t, sdk, llmName, foreignID, fingerprint)
				afterVolume, afterVolumeETag := readVolume()
				if etag != afterETag || !reflect.DeepEqual(before, after) || volumeETag != afterVolumeETag || !reflect.DeepEqual(volume, afterVolume) {
					t.Fatal("foreign collision changed guest or database volume")
				}
				assertStoppedPairGuestAbsent(t, sdk, pgName)
			}
		})
	}
	t.Run("accepted-stage-join", func(t *testing.T) {
		desc := environment.Descriptor{ID: "123e4567-e89b-42d3-a456-426614174013", Name: "accepted-gateway-fixture", Binding: binding}
		imageSpec := func(i gateway.NativeGatewayCreateImage) environment.GatewayImageSpec {
			return environment.GatewayImageSpec{Registry: i.Expected.Registry, Repository: i.Expected.Repository, Architecture: "amd64", ManifestDigest: i.Expected.ManifestDigest, ConfigDigest: i.Expected.ConfigDigest, IncusFingerprint: i.Expected.IncusFingerprint, Alias: i.Evidence.Alias}
		}
		c := environment.Candidate{APIVersion: environment.CandidateAPIVersion, Kind: environment.CandidateKind, Environment: desc,
			SelectedWorkloads: []string{}, Workloads: []acceptedstate.WorkloadSpec{}, Gateway: &environment.GatewaySpec{
				ManagementNetwork: "ag-pair-mgmt", EgressNetwork: "ag-pair-egress", EgressIPv4: "10.94.0.13/24", RootPool: stoppedPairPool, DatabasePool: stoppedPairPool,
				Postgres: imageSpec(images.Postgres), LiteLLM: imageSpec(images.LiteLLM), CredentialVersion: "fixture-v1", EnsureRunning: true,
			}}
		store, err := environment.InitializeLocalProjectStateStore(filepath.Join(t.TempDir(), "accepted"), binding)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.CommitLocalCandidate(ctx, desc, binding, nil, c); err != nil {
			t.Fatal(err)
		}
		rev, _, err := store.ReadLocalAccepted(ctx, desc)
		if err != nil || rev == nil {
			t.Fatalf("fixture accepted revision unavailable: %v", err)
		}
		plan := orchestration.NativePlan{APIVersion: orchestration.NativeAPIVersion, Kind: orchestration.NativePlanKind, Revision: *rev, Resources: []orchestration.NativeResource{
			{ID: orchestration.GatewayImageStage, Kind: orchestration.NativeGatewayImage},
			{ID: orchestration.GatewayVolumeStage, Kind: orchestration.NativeGatewayVolume, DependsOn: []string{orchestration.GatewayImageStage}},
			{ID: orchestration.GatewayInstanceStage, Kind: orchestration.NativeGatewayInstance, DependsOn: []string{orchestration.GatewayVolumeStage}},
		}}
		client := &joinedGatewayVMClient{stoppedPairSDK: stoppedPairSDK{InstanceServer: sdk}}
		exec := orchestration.LocalNativeExecution{Store: store, Environment: desc,
			CheckPolicy: orchestration.CheckPolicy{Mode: orchestration.ReadOnlyChecks, Authorize: func(_ context.Context, sc orchestration.TargetScope, ck check.Check) error {
				if sc.Deployment != desc.ID || ck.Effect != check.ReadOnly || !slices.Equal(ck.Access, []check.Access{check.IncusAccess}) {
					return errors.New("unexpected check capability")
				}
				return nil
			}},
			Authorize: func(_ context.Context, r orchestration.LocalNativeAuthorityRequest) (environment.Binding, error) {
				if r.Plan.Revision != *rev {
					return environment.Binding{}, errors.New("unexpected fixture revision")
				}
				if r.Permission == orchestration.LocalNativeSubmit && (r.Effect.Action != native.Create || !(r.Resource.ID == orchestration.GatewayVolumeStage && r.Effect.Target == "database" || r.Resource.ID == orchestration.GatewayInstanceStage && (r.Effect.Target == "postgres" || r.Effect.Target == "litellm"))) {
					return environment.Binding{}, errors.New("unexpected fixture effect")
				}
				return binding, nil
			},
			Bind: func(c environment.Candidate, plan orchestration.NativePlan) (map[string]orchestration.NativeResourceBinding, error) {
				return gateway.BindAcceptedGateway(c, plan, gateway.AcceptedGatewayDependencies{Client: client,
					Resolve: func(context.Context, nativeoci.Source) (nativeoci.ResolvedImage, error) {
						return nativeoci.ResolvedImage{}, errors.New("offline fixture has no registry")
					},
					ExistingImageEvidence: func(_ context.Context, pin environment.AcceptedRevision, role string) (native.GatewayImageEvidence, error) {
						if pin == *rev {
							switch role {
							case "postgres":
								return images.Postgres.Evidence, nil
							case "litellm":
								return images.LiteLLM.Evidence, nil
							}
						}
						return native.GatewayImageEvidence{}, errors.New("fixture image provenance unavailable")
					},
				})
			},
		}
		engine := orchestration.NewEngine(nil)
		preview, err := exec.ExecuteLocalNative(ctx, engine, plan, orchestration.ExecuteOptions{DryRun: true})
		if err != nil || len(preview.Native.Previews) != 3 || !preview.Native.Previews[2].Deferred || client.volumeWrites != 0 || len(client.instanceWrites) != 0 {
			t.Fatalf("fixture preview: %+v %v", preview.Native, err)
		}
		result, err := exec.ExecuteLocalNative(ctx, engine, plan, orchestration.ExecuteOptions{})
		if err != nil || len(result.Native.Effects) != 3 || len(result.Conditions) != 3 || result.Native.LocalOperationID == "" {
			t.Fatalf("fixture join: %+v %v", result.Native, err)
		}
		id, err := gateway.NativeGatewayIdentity(desc, binding)
		if err != nil {
			t.Fatal(err)
		}
		readStoppedPairGuest(t, sdk, id.PostgresInstance, desc.ID, images.Postgres.Expected.IncusFingerprint)
		readStoppedPairGuest(t, sdk, id.LiteLLMInstance, desc.ID, images.LiteLLM.Expected.IncusFingerprint)
		repeat, err := exec.ExecuteLocalNative(ctx, engine, plan, orchestration.ExecuteOptions{})
		if err != nil || len(repeat.Native.Effects) != 0 || client.volumeWrites != 1 || !slices.Equal(client.instanceWrites, []string{id.PostgresInstance, id.LiteLLMInstance}) {
			t.Fatalf("fixture repeat wrote or failed: %+v %v", repeat.Native, err)
		}
	})
}

type joinedGatewayVMClient struct{ stoppedPairSDK }

func (*joinedGatewayVMClient) CreateImage(api.ImagesPost) (native.GatewayImageAcquireOperation, error) {
	return nil, errors.New("offline fixture does not acquire images")
}

func createGatewayFixtureBridges(t *testing.T, admin incus.InstanceServer) {
	t.Helper()
	for _, bridge := range []struct{ name, resource, address, routed string }{
		{"ag-pair-mgmt", "management-network", "10.93.0.1/24", "false"},
		{"ag-pair-egress", "gateway-egress-network", "10.94.0.1/24", "true"},
	} {
		err := admin.UseProject("default").CreateNetwork(api.NetworksPost{
			Name: bridge.name, Type: "bridge", NetworkPut: api.NetworkPut{Config: map[string]string{
				"ipv4.address": bridge.address, "ipv4.dhcp": "false", "ipv6.address": "none",
				"ipv4.nat": bridge.routed, "ipv4.routing": bridge.routed,
				"user.aginctus.managed": "true", "user.aginctus.resource": bridge.resource,
			}},
		})
		if err != nil {
			t.Fatalf("fixture bridge %s: %v", bridge.name, err)
		}
	}
}

func stoppedPairCandidate(project string, images gateway.NativeGatewayCreateImages, egressAddress string) gateway.NativeGatewayTopology {
	pin := func(i native.GatewayOCIIdentity) string {
		return i.Registry + "/" + i.Repository + "@" + i.ManifestDigest
	}
	return gateway.NativeGatewayTopology{
		Deployment: "gateway", Project: project, ManagementNetwork: "ag-pair-mgmt", EgressNetwork: "ag-pair-egress", DatabaseVolume: "gateway-postgres",
		Services: []gateway.NativeGatewayService{
			{Name: "postgres", ImageReference: pin(images.Postgres.Expected), Volume: "gateway-postgres",
				Networks: []gateway.NativeGatewayNetwork{{Name: "ag-pair-mgmt"}}, SecretEnvironment: []string{"AGINCTUS_GATEWAY_POSTGRES_PASSWORD"}},
			{Name: "litellm", ImageReference: pin(images.LiteLLM.Expected), DependsOn: []string{"postgres"},
				Networks:          []gateway.NativeGatewayNetwork{{Name: "ag-pair-mgmt"}, {Name: "ag-pair-egress", IPv4Address: egressAddress}},
				SecretEnvironment: []string{"AGINCTUS_GATEWAY_POSTGRES_PASSWORD", "AGINCTUS_GATEWAY_MASTER_KEY", "AGINCTUS_GATEWAY_SALT_KEY"}},
		},
	}
}

func readStoppedPairGuest(t *testing.T, sdk incus.InstanceServer, name, environmentID, fingerprint string) (*api.Instance, string) {
	t.Helper()
	i, etag, err := sdk.GetInstance(name)
	if err != nil || i.Name != name || i.StatusCode != api.Stopped || i.Type != "container" || len(i.Profiles) != 0 ||
		i.Config["user.aginctus.environment-id"] != environmentID || i.Config["user.aginctus.resource"] != "gateway-instance" ||
		i.Config["volatile.base_image"] != fingerprint || i.Config["volatile.container.oci"] != "true" {
		t.Fatalf("guest %s: expected owned, profile-free stopped container from pinned image: %+v: %v", name, i, err)
	}
	for key := range i.Config {
		if strings.HasPrefix(key, "environment.") {
			t.Fatalf("unexpected runtime environment key %q on %s", key, name)
		}
	}
	return i, etag
}

func assertStoppedPairGuestAbsent(t *testing.T, sdk incus.InstanceServer, name string) {
	t.Helper()
	if _, _, err := sdk.GetInstance(name); !api.StatusErrorCheck(err, http.StatusNotFound) {
		t.Fatalf("expected %s absent: %v", name, err)
	}
}

// This is an offline, already-converted fixture, not OCI acquisition evidence.
// Hash the fixture declaration, runtime config and exact import bytes before
// consulting Incus. Never infer expected or evidence identity from a live alias.
func importGatewayFixtureImage(t *testing.T, ctx context.Context, sdk incus.InstanceServer, project, service string, executable []byte) gateway.NativeGatewayCreateImage {
	t.Helper()
	args := fmt.Sprintf(`"/not-a-runnable-service",%q`, service)
	fixture := "stopped-pair"
	if len(executable) > 0 {
		args = `"/stop-fixture"`
		fixture = "stop-retention"
	}
	config := fmt.Sprintf(`{"ociVersion":"1.0.2","process":{"args":[%s],"cwd":"/","user":{"uid":0,"gid":0}},"root":{"path":"rootfs"}}`, args)
	metadata := "architecture: x86_64\ncreation_date: 1700000000\nproperties:\n  type: oci\n  description: " + fixture + " fixture " + service + "\n"
	var image bytes.Buffer
	tarball := tar.NewWriter(&image)
	write := func(name string, mode int64, body []byte) {
		if err := tarball.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarball.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	write("metadata.yaml", 0o644, []byte(metadata))
	write("config.json", 0o644, []byte(config))
	write("rootfs/fixture-service", 0o644, []byte(service))
	if len(executable) > 0 {
		write("rootfs/stop-fixture", 0o755, executable)
	}
	if err := tarball.Close(); err != nil {
		t.Fatal(err)
	}
	identity := native.GatewayOCIIdentity{
		Registry: "stopped-fixture.invalid", Repository: "gateway/" + service,
		ConfigDigest:     fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(config))),
		IncusFingerprint: fmt.Sprintf("%x", sha256.Sum256(image.Bytes())),
	}
	identity.ManifestDigest = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(fixture+"-v1/"+service+"/"+identity.ConfigDigest+"/"+identity.IncusFingerprint)))
	op := waitStoppedPairOp(t, ctx, func() (incus.Operation, error) {
		return sdk.CreateImage(api.ImagesPost{}, &incus.ImageCreateArgs{MetaFile: bytes.NewReader(image.Bytes()), Type: "container"})
	})
	if op.Get().Metadata["fingerprint"] != identity.IncusFingerprint {
		t.Fatalf("imported %s fixture fingerprint differs from exact input bytes", service)
	}
	alias := service + "-" + fixture + "-fixture"
	if err := sdk.CreateImageAlias(api.ImageAliasesPost{ImageAliasesEntry: api.ImageAliasesEntry{
		Name: alias, ImageAliasesEntryPut: api.ImageAliasesEntryPut{Target: identity.IncusFingerprint},
	}}); err != nil {
		t.Fatal(err)
	}
	return gateway.NativeGatewayCreateImage{Expected: identity, Evidence: native.GatewayImageEvidence{Project: project, Alias: alias, Identity: identity}}
}

func waitStoppedPairOp(t *testing.T, ctx context.Context, submit func() (incus.Operation, error)) incus.Operation {
	t.Helper()
	op, err := submit()
	if err != nil {
		t.Fatal(err)
	}
	if err := op.WaitContext(ctx); err != nil {
		t.Fatal(err)
	}
	return op
}

// Count submissions forwarded to the real SDK, not successful operations.
type stoppedPairSDK struct {
	incus.InstanceServer
	volumeWrites   int
	instanceWrites []string
	afterSubmit    func()
}

func (c *stoppedPairSDK) CreateInstance(req api.InstancesPost) (native.InstanceCreateOperation, error) {
	c.instanceWrites = append(c.instanceWrites, req.Name)
	op, err := c.InstanceServer.CreateInstance(req)
	if err == nil && c.afterSubmit != nil {
		c.afterSubmit()
	}
	return op, err
}

func (c *stoppedPairSDK) CreateStoragePoolVolume(pool string, req api.StorageVolumesPost) error {
	c.volumeWrites++
	return c.InstanceServer.CreateStoragePoolVolume(pool, req)
}
