package nativelifecycle

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/managementnetwork"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	"github.com/blevinn/aginctus/internal/orchestration/nativeoci"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
	check "github.com/blevinn/aginctus/pkg/orchestration"
	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

// B and C have distinct fixture transport contracts but share one project
// inventory and submission log. Only the SDK and protected image evidence are
// synthetic; the test uses both real composers and D's real pin and journal.
type mixedGatewaySDK struct {
	*projectFixture
	id        gateway.NativeGatewayResourceIdentity
	serverPin string
	bridge    native.NetworkReader
	images    map[string]environment.GatewayImageSpec
	reads     int
}

func (g *mixedGatewaySDK) GetConnectionInfo() (*incus.ConnectionInfo, error) {
	g.reads++
	return &incus.ConnectionInfo{Protocol: "incus", Project: g.id.Project, SocketPath: "/fixture/incus.socket"}, nil
}

func (g *mixedGatewaySDK) GetServer() (*api.Server, string, error) {
	return &api.Server{Environment: api.ServerEnvironment{Project: g.id.Project, CertificateFingerprint: g.serverPin}}, "server", nil
}

func (g *mixedGatewaySDK) GetNetwork(name string) (*api.Network, string, error) {
	return g.bridge.GetNetwork(name)
}

func (g *mixedGatewaySDK) GetStoragePool(name string) (*api.StoragePool, string, error) {
	if name != "approved-pool" {
		g.t.Fatalf("unapproved pool read: %q", name)
	}
	return &api.StoragePool{Name: name, Status: "Created"}, "pool", nil
}

func (g *mixedGatewaySDK) GetStoragePoolVolume(pool, kind, name string) (*api.StorageVolume, string, error) {
	if pool != "approved-pool" || kind != "custom" || name != g.id.DatabaseVolume {
		g.t.Fatalf("unexpected gateway volume: %s %s %s", pool, kind, name)
	}
	if g.volume == nil {
		return nil, "", api.StatusErrorf(http.StatusNotFound, "absent")
	}
	return g.volume, "volume", nil
}

func (g *mixedGatewaySDK) CreateStoragePoolVolume(pool string, post api.StorageVolumesPost) error {
	if pool != "approved-pool" || post.Name != g.id.DatabaseVolume || g.volume != nil {
		g.t.Fatal("unexpected gateway volume submission")
	}
	g.submissions = append(g.submissions, "create:"+post.Name)
	g.volume = &api.StorageVolume{Name: post.Name, Project: g.id.Project, Type: post.Type, ContentType: post.ContentType, StorageVolumePut: post.StorageVolumePut}
	return nil
}

func (g *mixedGatewaySDK) GetImageAlias(name string) (*api.ImageAliasesEntry, string, error) {
	for _, image := range g.images {
		if image.Alias == name {
			return &api.ImageAliasesEntry{Name: name, ImageAliasesEntryPut: api.ImageAliasesEntryPut{Target: image.IncusFingerprint}}, "alias", nil
		}
	}
	g.t.Fatalf("unexpected gateway alias read: %q", name)
	return nil, "", nil
}

func (g *mixedGatewaySDK) GetImage(fingerprint string) (*api.Image, string, error) {
	for _, image := range g.images {
		if image.IncusFingerprint == fingerprint {
			return &api.Image{Fingerprint: fingerprint, Project: g.id.Project, Type: "container"}, "image", nil
		}
	}
	g.t.Fatalf("unexpected gateway image read: %q", fingerprint)
	return nil, "", nil
}

func (g *mixedGatewaySDK) GetImagesAllProjectsWithFilter([]string) ([]api.Image, error) {
	g.t.Fatal("pre-acquired images should not require daemon-wide image scan")
	return nil, nil
}

func (g *mixedGatewaySDK) CreateImage(api.ImagesPost) (native.GatewayImageAcquireOperation, error) {
	g.t.Fatal("unexpected gateway image acquisition")
	return nil, nil
}

func (g *mixedGatewaySDK) CreateImageAlias(api.ImageAliasesPost) error {
	g.t.Fatal("unexpected gateway alias creation")
	return nil
}

func (g *mixedGatewaySDK) GetInstance(name string) (*api.Instance, string, error) {
	if name != g.id.PostgresInstance && name != g.id.LiteLLMInstance {
		g.t.Fatalf("unexpected gateway instance read: %q", name)
	}
	if i, ok := g.instances[name]; ok {
		return &i, "instance", nil
	}
	return nil, "", api.StatusErrorf(http.StatusNotFound, "absent")
}

func (g *mixedGatewaySDK) CreateInstance(post api.InstancesPost) (native.InstanceCreateOperation, error) {
	role := orchestration.GatewayPostgresTarget
	if post.Name == g.id.LiteLLMInstance {
		role = orchestration.GatewayLiteLLMTarget
	} else if post.Name != g.id.PostgresInstance {
		g.t.Fatalf("unexpected gateway guest submission: %q", post.Name)
	}
	if _, exists := g.instances[post.Name]; exists || g.volume == nil || post.Source.Fingerprint != g.images[role].IncusFingerprint {
		g.t.Fatalf("gateway guest submitted outside the pinned order or with the wrong image: %q", post.Name)
	}
	g.submissions = append(g.submissions, "create:"+post.Name)
	return fixtureOperation{wait: func() error {
		post.Config["volatile.base_image"] = post.Source.Fingerprint
		g.instances[post.Name] = api.Instance{Name: post.Name, Project: g.id.Project, Type: string(post.Type), Status: "Stopped", CreatedAt: time.Now(), InstancePut: post.InstancePut, ExpandedDevices: post.Devices}
		if role == orchestration.GatewayPostgresTarget {
			g.volume.UsedBy = []string{"/1.0/instances/" + post.Name + "?project=" + g.id.Project}
		}
		return nil
	}}, nil
}

func TestMixedNativeBindingDenialAndRecovery(t *testing.T) {
	for _, scenario := range []string{"deny-full-plan", "foreign-workload", "recover"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := t.Context()
			f := newFixture(t)
			desc := f.accepted.Environment
			bridge := *f.selection.Bridge
			f.accepted.Workloads[0].Native = &acceptedstate.WorkloadNativeSpec{Runtime: "pinned-runtime", StoragePool: "approved-pool", ManagementNetwork: bridge.Spec.Name}
			image := func(repository, alias, hex string) environment.GatewayImageSpec {
				return environment.GatewayImageSpec{Registry: "docker.io", Repository: repository, Alias: alias, Architecture: "amd64", IncusFingerprint: strings.Repeat(hex, 64), ManifestDigest: "sha256:" + strings.Repeat(hex, 64), ConfigDigest: "sha256:" + strings.Repeat("e", 64)}
			}
			pg, app := image("library/postgres", "fixture-pg", "b"), image("berriai/litellm", "fixture-llm", "c")
			f.accepted.Gateway = &environment.GatewaySpec{ManagementNetwork: bridge.Spec.Name, RootPool: "approved-pool", DatabasePool: "approved-pool", CredentialVersion: "fixture-v1", Postgres: pg, LiteLLM: app}
			store, err := environment.InitializeLocalProjectStateStore(filepath.Join(t.TempDir(), "project"), desc.Binding)
			if err != nil {
				t.Fatal(err)
			}
			rev, err := store.PromoteLocalCandidate(ctx, desc, nil, f.accepted, func(context.Context, environment.LocalPromotionRequest) (environment.Binding, error) {
				return desc.Binding, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			plan := orchestration.NativePlan{APIVersion: orchestration.NativeAPIVersion, Kind: orchestration.NativePlanKind, Revision: rev, Resources: []orchestration.NativeResource{
				{ID: "agent", Kind: orchestration.NativeWorkload, DependsOn: []string{orchestration.GatewayInstanceStage, ManagementNetworkID}},
				{ID: ManagementNetworkID, Kind: orchestration.NativeManagementNetwork},
				{ID: orchestration.GatewayInstanceStage, Kind: orchestration.NativeGatewayInstance, DependsOn: []string{orchestration.GatewayVolumeStage}},
				{ID: orchestration.GatewayVolumeStage, Kind: orchestration.NativeGatewayVolume, DependsOn: []string{orchestration.GatewayImageStage, ManagementNetworkID}},
				{ID: orchestration.GatewayImageStage, Kind: orchestration.NativeGatewayImage},
			}}
			id, err := gateway.NativeGatewayIdentity(desc, desc.Binding)
			if err != nil {
				t.Fatal(err)
			}
			g := &mixedGatewaySDK{projectFixture: f.project, id: id, serverPin: f.pin, bridge: bridge.Reader, images: map[string]environment.GatewayImageSpec{orchestration.GatewayPostgresTarget: pg, orchestration.GatewayLiteLLMTarget: app}}
			// The gateway has a local fixture view of the same server pinned by C's
			// restricted connection. Neither transport opens an SDK connection.
			var bridgeReads, executeChecks, submitChecks int
			verifyBridge := bridge.VerifyBinding
			bridge.VerifyBinding = func(ctx context.Context, b environment.Binding, s managementnetwork.Spec) error {
				bridgeReads++
				return verifyBridge(ctx, b, s)
			}
			composer := LocalComposer{Environment: desc, Bridge: bridge, TrustedServerPin: f.pin, Client: f.project, VerifyAttachments: f.verify}
			var fragments []string
			exec := orchestration.LocalNativeExecution{Store: store, Environment: desc,
				BindLifecycle: func(c environment.Candidate, p orchestration.NativePlan, bootstrap bool) (orchestration.NativePlan, map[string]orchestration.NativeResourceBinding, error) {
					fragments = append(fragments, "lifecycle")
					return composer.BindLifecycle(c, p, bootstrap)
				},
				Bind: func(c environment.Candidate, p orchestration.NativePlan) (map[string]orchestration.NativeResourceBinding, error) {
					fragments = append(fragments, "gateway")
					return gateway.BindAcceptedGateway(c, p, gateway.AcceptedGatewayDependencies{Client: g,
						Resolve: func(context.Context, nativeoci.Source) (nativeoci.ResolvedImage, error) {
							return nativeoci.ResolvedImage{}, errors.New("unexpected registry access")
						},
						ExistingImageEvidence: func(_ context.Context, accepted environment.AcceptedRevision, role string) (native.GatewayImageEvidence, error) {
							i, ok := g.images[role]
							if accepted != rev || !ok {
								return native.GatewayImageEvidence{}, errors.New("wrong image evidence pin or role")
							}
							return native.GatewayImageEvidence{Project: desc.Binding.Project, Alias: i.Alias, Identity: native.GatewayOCIIdentity{Registry: i.Registry, Repository: i.Repository, ManifestDigest: i.ManifestDigest, ConfigDigest: i.ConfigDigest, IncusFingerprint: i.IncusFingerprint}}, nil
						},
					})
				},
				Authorize: func(_ context.Context, req orchestration.LocalNativeAuthorityRequest) (environment.Binding, error) {
					wantOrder := []string{orchestration.GatewayImageStage, ManagementNetworkID, orchestration.GatewayVolumeStage, orchestration.GatewayInstanceStage, "agent"}
					if req.Plan.Revision != rev || req.Environment != desc || !slices.EqualFunc(req.Plan.Resources, wantOrder, func(r orchestration.NativeResource, id string) bool { return r.ID == id }) ||
						!slices.Equal(req.Plan.Resources[2].DependsOn, []string{orchestration.GatewayImageStage, ManagementNetworkID}) ||
						!slices.Equal(req.Plan.Resources[4].DependsOn, []string{orchestration.GatewayInstanceStage, ManagementNetworkID}) {
						t.Fatalf("authority lost the full graph or pin: %+v", req)
					}
					if req.Permission == orchestration.LocalNativeExecute {
						executeChecks++
						if scenario == "deny-full-plan" && executeChecks == 2 {
							return environment.Binding{}, errors.New("mixed plan denied at engine authorization")
						}
					}
					if req.Permission == orchestration.LocalNativeSubmit {
						submitChecks++
						if req.Resource.ID == "agent" && (g.instances[id.PostgresInstance].Status != "Stopped" || g.instances[id.LiteLLMInstance].Status != "Stopped") {
							t.Fatal("workload submitted before the gateway prerequisite")
						}
					}
					return desc.Binding, nil
				},
				CheckPolicy: orchestration.CheckPolicy{Mode: orchestration.ReadOnlyChecks, Authorize: func(context.Context, orchestration.TargetScope, check.Check) error { return nil }},
			}
			engine := orchestration.NewEngine(nil)
			if scenario == "foreign-workload" {
				f.project.instances["agent-1"] = api.Instance{Name: "agent-1", Project: desc.Binding.Project, Status: "Stopped"}
			}
			if scenario != "recover" {
				r, err := exec.ExecuteLocalNative(ctx, engine, plan, orchestration.ExecuteOptions{})
				ids, readErr := store.ListLocalNativeOperationIDs(ctx, desc)
				if err == nil || readErr != nil || len(ids) != 0 || len(f.project.submissions) != 0 || submitChecks != 0 || !slices.Equal(fragments, []string{"lifecycle", "gateway"}) {
					t.Fatalf("mixed denial leaked effects: %+v, %v; ids=%v, %v; writes=%v; binds=%v", r, err, ids, readErr, f.project.submissions, fragments)
				}
				if scenario == "deny-full-plan" && (g.reads != 0 || bridgeReads != 0 || executeChecks != 2) {
					t.Fatal("full-plan refusal reached SDK or did not run at the engine boundary")
				}
				if scenario == "foreign-workload" && (g.reads == 0 || bridgeReads == 0 || r.Native == nil || len(r.Native.Previews) != 4) {
					t.Fatalf("expected a late lifecycle preview to block the already-planned gateway effects: %+v, %v", r.Native, err)
				}
				return
			}
			preview, err := exec.ExecuteLocalNative(ctx, engine, plan, orchestration.ExecuteOptions{DryRun: true})
			if err != nil || preview.Native == nil || preview.Native.Revision != rev || len(preview.Native.Previews) != 5 || len(preview.Native.Effects) != 0 || len(f.project.submissions) != 0 || submitChecks != 0 {
				t.Fatalf("mixed dry-run: %+v, %v", preview, err)
			}
			f.project.failWait = true // Create reaches the SDK, but the wait loses its result.
			failed, err := exec.ExecuteLocalNative(ctx, engine, plan, orchestration.ExecuteOptions{})
			want := []string{"create:" + id.DatabaseVolume, "create:" + id.PostgresInstance, "create:" + id.LiteLLMInstance, "create:agent-1"}
			if err == nil || failed.Native == nil || !slices.Equal(f.project.submissions, want) || len(failed.Native.Effects) != len(want) || len(failed.Conditions) != 4 {
				t.Fatalf("lost mixed prefix: %+v, %v; writes=%v", failed, err, f.project.submissions)
			}
			for i, stage := range []string{orchestration.GatewayImageStage, ManagementNetworkID, orchestration.GatewayVolumeStage, orchestration.GatewayInstanceStage} {
				c := failed.Conditions[i]
				if c.StepID != stage || c.Phase != orchestration.PostconditionPhase || c.Finding.Outcome != check.Passed || c.Finding.AcceptedRevision != rev.ID {
					t.Fatalf("completed prerequisite lacked pinned evidence: %+v", c)
				}
			}
			firstID := failed.Native.LocalOperationID
			journal, err := store.ReadLocalNativeOperation(ctx, desc, firstID)
			if err != nil || journal.Revision != rev || journal.Status != environment.LocalNativeFailed || len(journal.Effects) != len(want) {
				t.Fatalf("missing failed mixed history: %+v, %v", journal, err)
			}
			for n, resource := range []string{orchestration.GatewayVolumeStage, orchestration.GatewayInstanceStage, orchestration.GatewayInstanceStage, "agent"} {
				outcome := environment.LocalNativeApplied
				if resource == "agent" {
					outcome = environment.LocalNativeUncertain
				}
				if e := journal.Effects[n]; e.Resource != resource || e.Action != "create" || e.Outcome != outcome {
					t.Fatalf("wrong mixed recovery evidence at %d: %+v", n, e)
				}
			}
			volume := g.volume
			f.project.failWait = false
			if _, err := exec.ExecuteLocalNative(ctx, engine, plan, orchestration.ExecuteOptions{}); !errors.Is(err, environment.ErrLocalNativeUnresolved) || !slices.Equal(f.project.submissions, want) {
				t.Fatalf("competing writer resumed before terminal verification: %v", err)
			}
			// The injected SDK's create is synchronous and already returned its
			// lost-wait error; the fixture has no daemon job that can still run.
			if err := store.ResolveLocalNativeOperation(ctx, desc, rev, firstID, func(_ context.Context, request environment.LocalNativeRecoveryRequest) (environment.Binding, error) {
				if request.Environment != desc || request.CurrentRevision != rev || request.Operation.ID != firstID ||
					!slices.Equal(request.Operation.Effects, journal.Effects) || !slices.Equal(f.project.submissions, want) {
					return environment.Binding{}, errors.New("fixture terminal evidence unverified")
				}
				return desc.Binding, nil
			}); err != nil {
				t.Fatal(err)
			}
			recovered, err := exec.ExecuteLocalNative(ctx, engine, plan, orchestration.ExecuteOptions{})
			if err != nil || recovered.Native == nil || recovered.Native.Revision != rev || recovered.Native.LocalOperationID == firstID || len(recovered.Native.Effects) != 1 || len(recovered.Conditions) != 5 || !slices.Equal(f.project.submissions, append(want, "start:agent-1")) || g.volume != volume || f.project.instances["agent-1"].Status != "Running" {
				t.Fatalf("recovery did not resume from observation: %+v, %v; writes=%v", recovered, err, f.project.submissions)
			}
			old, err := store.ReadLocalNativeOperation(ctx, desc, firstID)
			if err != nil || old.Status != journal.Status || !slices.Equal(old.Effects, journal.Effects) {
				t.Fatalf("recovery rewrote uncertain history: %+v, %v", old, err)
			}
		})
	}
}
