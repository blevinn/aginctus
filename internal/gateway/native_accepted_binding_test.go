package gateway

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	"github.com/blevinn/aginctus/internal/orchestration/nativeoci"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
	check "github.com/blevinn/aginctus/pkg/orchestration"
	"github.com/lxc/incus/v7/shared/api"
)

type acceptedGatewayFixtureClient struct {
	*fakeGatewayStoppedClient
	server         *api.Server
	mutations      []string
	imagesBySource map[string]environment.GatewayImageSpec
	before         func(string)
}

func (c *acceptedGatewayFixtureClient) GetServer() (*api.Server, string, error) {
	return c.server, "etag", nil
}
func (c *acceptedGatewayFixtureClient) GetStoragePoolVolume(pool, kind, name string) (*api.StorageVolume, string, error) {
	if c.volume == nil {
		return nil, "", api.StatusErrorf(http.StatusNotFound, "missing")
	}
	return c.fakeGatewayStoppedClient.GetStoragePoolVolume(pool, kind, name)
}
func (c *acceptedGatewayFixtureClient) mutate(s string) {
	c.before(s)
	c.mutations = append(c.mutations, s)
}
func (c *acceptedGatewayFixtureClient) CreateStoragePoolVolume(pool string, in api.StorageVolumesPost) error {
	c.mutate("volume:" + pool + "/" + in.Name)
	c.volume = &api.StorageVolume{Name: in.Name, Type: in.Type, ContentType: in.ContentType, Project: c.server.Environment.Project, StorageVolumePut: in.StorageVolumePut}
	return nil
}
func (c *acceptedGatewayFixtureClient) CreateInstance(in api.InstancesPost) (native.InstanceCreateOperation, error) {
	c.mutate("guest:" + in.Name)
	return c.fakeGatewayStoppedClient.CreateInstance(in)
}
func (c *acceptedGatewayFixtureClient) GetImagesAllProjectsWithFilter([]string) ([]api.Image, error) {
	var out []api.Image
	for _, img := range c.images {
		out = append(out, *img)
	}
	return out, nil
}

type joinedImageOperation struct {
	successfulGatewayCreateOperation
	fingerprint string
}

func (op joinedImageOperation) Get() api.Operation {
	return api.Operation{Metadata: map[string]any{"fingerprint": op.fingerprint}}
}
func (c *acceptedGatewayFixtureClient) CreateImage(in api.ImagesPost) (native.GatewayImageAcquireOperation, error) {
	s := in.Source.Server + "/" + in.Source.Alias
	c.mutate("image:" + s)
	spec, ok := c.imagesBySource[s]
	if !ok {
		return nil, errors.New("unexpected image source")
	}
	c.images[spec.IncusFingerprint] = &api.Image{Project: c.server.Environment.Project, Fingerprint: spec.IncusFingerprint, Type: "container"}
	return joinedImageOperation{fingerprint: spec.IncusFingerprint}, nil
}
func (c *acceptedGatewayFixtureClient) CreateImageAlias(in api.ImageAliasesPost) error {
	c.mutate("alias:" + in.Name + "/" + in.Target)
	c.aliases[in.Name] = &in.ImageAliasesEntry
	return nil
}

func TestAcceptedGatewayJoinsPinnedAuthorityWithConcreteSDKOperations(t *testing.T) {
	for _, scenario := range []string{"apply", "foreign-guest", "foreign-alias", "registry-mismatch", "deny-alias", "runtime"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := t.Context()
			top, devices, pair, base := environmentGatewayCreateFixture(t)
			desc := startFixtureDescriptor(top)
			toSpec := func(in NativeGatewayCreateImage) environment.GatewayImageSpec {
				return environment.GatewayImageSpec{Registry: in.Expected.Registry, Repository: in.Expected.Repository, Architecture: "amd64", ManifestDigest: in.Expected.ManifestDigest, ConfigDigest: in.Expected.ConfigDigest, IncusFingerprint: in.Expected.IncusFingerprint, Alias: in.Evidence.Alias}
			}
			g := &environment.GatewaySpec{ManagementNetwork: top.ManagementNetwork, EgressNetwork: top.EgressNetwork, EgressIPv4: top.Services[1].Networks[1].IPv4Address,
				RootPool: devices.Postgres["root"]["pool"], DatabasePool: devices.Postgres["database"]["pool"], CredentialVersion: "fixture-v1", EnsureRunning: true,
				Postgres: toSpec(base.imagesForCreate.Postgres), LiteLLM: toSpec(base.imagesForCreate.LiteLLM)}
			candidate := environment.Candidate{APIVersion: environment.CandidateAPIVersion, Kind: environment.CandidateKind, Environment: desc, Gateway: g, SelectedWorkloads: []string{}, Workloads: []acceptedstate.WorkloadSpec{}}
			store, err := environment.InitializeLocalProjectStateStore(filepath.Join(t.TempDir(), "project"), desc.Binding)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.PromoteLocalCandidate(ctx, desc, nil, candidate, func(_ context.Context, r environment.LocalPromotionRequest) (environment.Binding, error) {
				if r.Environment != desc || r.Candidate.Gateway == nil || *r.Candidate.Gateway != *g {
					return environment.Binding{}, errors.New("unexpected candidate promotion")
				}
				return desc.Binding, nil
			}); err != nil {
				t.Fatal(err)
			}
			rev, _, err := store.ReadLocalAccepted(ctx, desc)
			if err != nil || rev == nil {
				t.Fatalf("pinned candidate unavailable: %v", err)
			}
			plan := orchestration.NativePlan{APIVersion: orchestration.NativeAPIVersion, Kind: orchestration.NativePlanKind, Revision: *rev, Resources: []orchestration.NativeResource{
				{ID: orchestration.GatewayImageStage, Kind: orchestration.NativeGatewayImage},
				{ID: orchestration.GatewayVolumeStage, Kind: orchestration.NativeGatewayVolume, DependsOn: []string{orchestration.GatewayImageStage}},
				{ID: orchestration.GatewayInstanceStage, Kind: orchestration.NativeGatewayInstance, DependsOn: []string{orchestration.GatewayVolumeStage}},
			}}
			if scenario == "runtime" {
				plan.Resources = append(plan.Resources, orchestration.NativeResource{ID: orchestration.GatewayRuntimeStage, Kind: orchestration.NativeGatewayRuntime, DependsOn: []string{orchestration.GatewayInstanceStage}})
			}
			clear(base.images)
			clear(base.aliases)
			base.volume = nil
			if scenario == "foreign-guest" {
				base.instances[pair.LiteLLM.Name] = &api.Instance{Name: pair.LiteLLM.Name, Project: top.Project, Status: "Stopped"}
			}
			if scenario == "foreign-alias" {
				base.aliases[g.LiteLLM.Alias] = &api.ImageAliasesEntry{Name: g.LiteLLM.Alias, ImageAliasesEntryPut: api.ImageAliasesEntryPut{Target: strings.Repeat("f", 64)}}
			}
			last := ""
			client := &acceptedGatewayFixtureClient{fakeGatewayStoppedClient: base, server: &api.Server{Environment: api.ServerEnvironment{Project: top.Project, CertificateFingerprint: strings.TrimPrefix(desc.Binding.ServerID, "sha256:")}}, imagesBySource: map[string]environment.GatewayImageSpec{}, before: func(s string) {
				if s != last {
					t.Fatalf("SDK submission not authorized for exact target: %s; last grant: %s", s, last)
				}
				last = ""
			}}
			source := func(i environment.GatewayImageSpec) string {
				return "https://" + i.Registry + "/" + i.Repository + "@" + i.ManifestDigest
			}
			client.imagesBySource[source(g.Postgres)] = g.Postgres
			client.imagesBySource[source(g.LiteLLM)] = g.LiteLLM
			want := []string{"image:" + source(g.Postgres), "alias:" + g.Postgres.Alias + "/" + g.Postgres.IncusFingerprint, "image:" + source(g.LiteLLM), "alias:" + g.LiteLLM.Alias + "/" + g.LiteLLM.IncusFingerprint, "volume:" + g.DatabasePool + "/" + top.DatabaseVolume, "guest:" + pair.Postgres.Name, "guest:" + pair.LiteLLM.Name}
			reuse := false
			deps := AcceptedGatewayDependencies{Client: client,
				Resolve: func(_ context.Context, s nativeoci.Source) (nativeoci.ResolvedImage, error) {
					for _, i := range []environment.GatewayImageSpec{g.Postgres, g.LiteLLM} {
						if reflect.DeepEqual(s, i.Source()) {
							r := nativeoci.ResolvedImage{Registry: i.Registry, Repository: i.Repository, ManifestDigest: i.ManifestDigest, ConfigDigest: i.ConfigDigest, IncusFingerprint: i.IncusFingerprint}
							if scenario == "registry-mismatch" && i.Alias == g.LiteLLM.Alias {
								r.ConfigDigest = strings.Repeat("f", 64)
							}
							return r, nil
						}
					}
					return nativeoci.ResolvedImage{}, errors.New("unexpected OCI source")
				}, ExistingImageEvidence: func(_ context.Context, r environment.AcceptedRevision, role string) (native.GatewayImageEvidence, error) {
					if !reuse || r != *rev {
						return native.GatewayImageEvidence{}, errors.New("unproven fixture image")
					}
					if role == "postgres" {
						return base.imagesForCreate.Postgres.Evidence, nil
					}
					return base.imagesForCreate.LiteLLM.Evidence, nil
				},
			}
			exec := orchestration.LocalNativeExecution{Store: store, Environment: desc,
				CheckPolicy: orchestration.CheckPolicy{Mode: orchestration.ReadOnlyChecks, Authorize: func(context.Context, orchestration.TargetScope, check.Check) error { return nil }},
				Bind: func(c environment.Candidate, p orchestration.NativePlan) (map[string]orchestration.NativeResourceBinding, error) {
					return BindAcceptedGateway(c, p, deps)
				},
				Authorize: func(_ context.Context, r orchestration.LocalNativeAuthorityRequest) (environment.Binding, error) {
					if r.Permission == orchestration.LocalNativeSubmit {
						if r.Plan.Revision != *rev || r.Effect.Action != native.Create {
							t.Fatal("wrong revision or action")
						}
						var index int
						switch r.Resource.ID + "/" + r.Effect.Target {
						case "gateway-image/postgres":
							index = 0
						case "gateway-image/postgres-alias":
							index = 1
						case "gateway-image/litellm":
							index = 2
						case "gateway-image/litellm-alias":
							index = 3
						case "gateway-volume/database":
							index = 4
						case "gateway-instance/postgres":
							index = 5
						case "gateway-instance/litellm":
							index = 6
						default:
							t.Fatal("unexpected effect target")
						}
						if scenario == "deny-alias" && index == 1 {
							return environment.Binding{}, errors.New("revoked")
						}
						last = want[index]
					}
					return desc.Binding, nil
				},
			}
			engine := orchestration.NewEngine(nil)
			preview, err := exec.ExecuteLocalNative(ctx, engine, plan, orchestration.ExecuteOptions{DryRun: true})
			if scenario == "foreign-guest" || scenario == "foreign-alias" || scenario == "registry-mismatch" || scenario == "runtime" {
				if err == nil || len(client.mutations) != 0 {
					t.Fatalf("unsafe preview: %v", err)
				}
				if _, err := exec.ExecuteLocalNative(ctx, engine, plan, orchestration.ExecuteOptions{}); err == nil || len(client.mutations) != 0 {
					t.Fatalf("unsafe apply: %v", err)
				}
				return
			}
			if err != nil || preview.Native == nil || len(preview.Native.Previews) != 3 || len(client.mutations) != 0 || last != "" {
				t.Fatalf("join preview: %+v %v", preview.Native, err)
			}
			if len(preview.Native.Previews[0].Actions) != 4 || !preview.Native.Previews[1].Deferred || !preview.Native.Previews[2].Deferred || len(preview.Native.Previews[2].Actions) != 2 {
				t.Fatalf("incomplete preview: %+v", preview.Native.Previews)
			}
			result, err := exec.ExecuteLocalNative(ctx, engine, plan, orchestration.ExecuteOptions{})
			if scenario == "deny-alias" {
				if err == nil || !slices.Equal(client.mutations, want[:1]) || len(result.Native.Effects) != 2 || result.Native.Effects[1].Outcome != orchestration.NativeEffectBlocked {
					t.Fatalf("lost effect denial: %+v %v", result.Native, err)
				}
				if _, err := exec.ExecuteLocalNative(ctx, engine, plan, orchestration.ExecuteOptions{}); err == nil || len(client.mutations) != 1 {
					t.Fatal("orphan image was adopted or retry submitted writes")
				}
				return
			}
			if err != nil || !slices.Equal(client.mutations, want) || len(result.Conditions) != 3 || result.Native.LocalOperationID == "" {
				t.Fatalf("joined execution: %+v %v mutations=%v", result, err, client.mutations)
			}
			journal, err := store.ReadLocalNativeOperation(ctx, desc, result.Native.LocalOperationID)
			if err != nil || len(journal.Effects) != len(want) {
				t.Fatalf("missing durable effect records: %+v %v", journal, err)
			}
			for _, name := range []string{pair.Postgres.Name, pair.LiteLLM.Name} {
				if client.instances[name].Status != "Stopped" {
					t.Fatal("guest was not left stopped")
				}
			}
			reuse = true
			repeat, err := exec.ExecuteLocalNative(ctx, engine, plan, orchestration.ExecuteOptions{})
			if err != nil || len(repeat.Native.Effects) != 0 || !slices.Equal(client.mutations, want) {
				t.Fatalf("verified repeat was not a no-op: %+v %v", repeat.Native, err)
			}
		})
	}
}
