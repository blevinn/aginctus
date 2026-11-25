package gateway

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration/native"
	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

// Synthetic combined fake is stricter than a no-op operation: every create
// changes its observed inventory and carries the same owned device markers.
type fakeGatewayStoppedClient struct {
	*fakeGatewayInstanceReader
	*fakeGatewayAttachmentReader
	created         []string
	imagesForCreate NativeGatewayCreateImages
	onCreate        func(api.InstancesPost) error
	onGetInstance   func(string)
}

func (f *fakeGatewayStoppedClient) GetConnectionInfo() (*incusclient.ConnectionInfo, error) {
	return f.fakeGatewayInstanceReader.GetConnectionInfo()
}
func (f *fakeGatewayStoppedClient) GetProject(s string) (*api.Project, string, error) {
	return f.fakeGatewayInstanceReader.GetProject(s)
}
func (f *fakeGatewayStoppedClient) GetInstance(name string) (*api.Instance, string, error) {
	if f.onGetInstance != nil {
		f.onGetInstance(name)
	}
	return f.fakeGatewayInstanceReader.GetInstance(name)
}

type successfulGatewayCreateOperation struct{}

func (successfulGatewayCreateOperation) WaitContext(context.Context) error { return nil }
func (f *fakeGatewayStoppedClient) CreateInstance(req api.InstancesPost) (native.InstanceCreateOperation, error) {
	f.created = append(f.created, req.Name)
	if f.onCreate != nil {
		if err := f.onCreate(req); err != nil {
			return nil, err
		}
	}
	fingerprint := req.Source.Fingerprint
	f.instances[req.Name] = &api.Instance{
		Name:    req.Name,
		Project: f.fakeGatewayInstanceReader.info.Project,
		Type:    string(api.InstanceTypeContainer),
		Status:  "Stopped",
		InstancePut: api.InstancePut{
			Description: req.Description, Devices: req.Devices, Profiles: req.Profiles,
			Config: map[string]string{
				"user.aginctus.managed": "true", "user.aginctus.resource": "gateway-instance",
				"user.aginctus.role":           req.Config["user.aginctus.role"],
				"user.aginctus.deployment":     req.Config["user.aginctus.deployment"],
				"user.aginctus.environment-id": req.Config["user.aginctus.environment-id"],
				"volatile.base_image":          fingerprint,
			},
		},
	}
	return successfulGatewayCreateOperation{}, nil
}

func stoppedImageFixture(pair NativeGatewayInstancePair) NativeGatewayCreateImages {
	pin := func(spec NativeGatewayInstanceSpec, registry, repository, fingerprint string) NativeGatewayCreateImage {
		identity := native.GatewayOCIIdentity{
			Registry: registry, Repository: repository, ManifestDigest: "sha256:" + strings.Repeat("a", 64),
			ConfigDigest: "sha256:" + strings.Repeat("c", 64), IncusFingerprint: strings.Repeat(fingerprint, 64),
		}
		return NativeGatewayCreateImage{Expected: identity, Evidence: native.GatewayImageEvidence{
			Project: spec.Project, Alias: spec.ImageAlias, Identity: identity,
		}}
	}
	return NativeGatewayCreateImages{
		Postgres: pin(pair.Postgres, "docker.io", "library/postgres", "a"),
		LiteLLM:  pin(pair.LiteLLM, "ghcr.io", "berriai/litellm", "b"),
	}
}

func stoppedClientFixture(t *testing.T) (NativeGatewayTopology, NativeGatewayDevices, NativeGatewayInstancePair, *fakeGatewayStoppedClient) {
	t.Helper()
	top, devices, att := nativeAttachmentAuditFixture(t)
	_, reader := instanceReaderFixture(t)
	pair, err := PlanNativeGatewayInstances(top, devices, "pg-image", "llm-image")
	if err != nil {
		t.Fatal(err)
	}
	return top, devices, pair, &fakeGatewayStoppedClient{fakeGatewayInstanceReader: reader, fakeGatewayAttachmentReader: att, imagesForCreate: stoppedImageFixture(pair)}
}
func TestCreateStoppedGatewayGuardedOrdered(t *testing.T) {
	top, devices, pair, client := stoppedClientFixture(t)
	wantFingerprint := map[string]string{
		pair.Postgres.Name: client.imagesForCreate.Postgres.Expected.IncusFingerprint,
		pair.LiteLLM.Name:  client.imagesForCreate.LiteLLM.Expected.IncusFingerprint,
	}
	client.onCreate = func(req api.InstancesPost) error {
		want, ok := wantFingerprint[req.Name]
		if !ok || req.Source.Type != "image" || req.Source.Fingerprint != want || req.Source.Alias != "" || req.Source.Server != "" {
			t.Fatalf("create did not use the exact pinned local fingerprint: %+v", req.Source)
		}
		return nil
	}
	guards := 0
	result, err := CreateStoppedGatewayWithClient(context.Background(), top, devices, pair, client.imagesForCreate, client, func(context.Context) error { guards++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Created) != 2 || result.Created[0] != "postgres-1" || result.Created[1] != "litellm-1" ||
		len(result.Verified) != 2 || guards != 2 {
		t.Fatalf("ordered result: %+v guards=%d", result, guards)
	}
	result, err = CreateStoppedGatewayWithClient(context.Background(), top, devices, pair, client.imagesForCreate, client, func(context.Context) error { guards++; return nil })
	if err != nil || len(result.Created) != 0 || len(result.Verified) != 2 || guards != 2 || len(client.created) != 2 {
		t.Fatalf("repeat: %+v %v", result, err)
	}
}
func TestCreateStoppedGatewayRejectsNoAuthorityAndDrift(t *testing.T) {
	top, devices, pair, client := stoppedClientFixture(t)
	if _, err := CreateStoppedGatewayWithClient(context.Background(), top, devices, pair, client.imagesForCreate, client, nil); err == nil || len(client.created) != 0 {
		t.Fatal("nil guard permitted a write")
	}
	// Caller cannot smuggle an arbitrary extra host mount.
	pair.Postgres.Devices["host"] = map[string]string{"type": "disk", "source": "/etc", "path": "/mnt"}
	if _, err := CreateStoppedGatewayWithClient(context.Background(), top, devices, pair, client.imagesForCreate, client, func(context.Context) error { return nil }); err == nil {
		t.Fatal("unapproved attachment accepted")
	}
}
func TestCreateStoppedGatewayStopsAfterPartialFailure(t *testing.T) {
	top, devices, pair, client := stoppedClientFixture(t)
	client.onCreate = func(req api.InstancesPost) error {
		if req.Name == "litellm-1" {
			return fmt.Errorf("synthetic submission failure")
		}
		return nil
	}
	result, err := CreateStoppedGatewayWithClient(context.Background(), top, devices, pair, client.imagesForCreate, client, func(context.Context) error { return nil })
	var stoppedErr *NativeGatewayStoppedError
	if !errors.As(err, &stoppedErr) || stoppedErr.Stage != "create" || !stoppedErr.Attempted || !stoppedErr.Uncertain ||
		!slices.Equal(result.Created, []string{pair.Postgres.Name}) || !slices.Equal(result.Verified, result.Created) {
		t.Fatalf("partial completion: %+v %v", result, err)
	}
	if client.instances["postgres-1"] == nil || client.instances["litellm-1"] != nil {
		t.Fatal("unexpected rollback or hidden second create")
	}
}

func TestCreateStoppedGatewayRejectsUnverifiedImagePair(t *testing.T) {
	for name, drift := range map[string]func(*fakeGatewayStoppedClient, *NativeGatewayCreateImage){
		"missing expected identity": func(_ *fakeGatewayStoppedClient, pin *NativeGatewayCreateImage) {
			pin.Expected = native.GatewayOCIIdentity{}
		},
		"missing evidence": func(_ *fakeGatewayStoppedClient, pin *NativeGatewayCreateImage) {
			pin.Evidence = native.GatewayImageEvidence{}
		},
		"wrong evidence project": func(_ *fakeGatewayStoppedClient, pin *NativeGatewayCreateImage) {
			pin.Evidence.Project = "other"
		},
		"wrong evidence alias": func(_ *fakeGatewayStoppedClient, pin *NativeGatewayCreateImage) {
			pin.Evidence.Alias = "other"
		},
		"same layers different config": func(_ *fakeGatewayStoppedClient, pin *NativeGatewayCreateImage) {
			pin.Evidence.Identity.ConfigDigest = "sha256:" + strings.Repeat("d", 64)
		},
		"same layers different manifest": func(_ *fakeGatewayStoppedClient, pin *NativeGatewayCreateImage) {
			pin.Evidence.Identity.ManifestDigest = "sha256:" + strings.Repeat("d", 64)
		},
		"expected and evidence diverge from topology": func(_ *fakeGatewayStoppedClient, pin *NativeGatewayCreateImage) {
			pin.Expected.ManifestDigest = "sha256:" + strings.Repeat("d", 64)
			pin.Evidence.Identity = pin.Expected
		},
		"wrong image project": func(client *fakeGatewayStoppedClient, pin *NativeGatewayCreateImage) {
			client.images[pin.Expected.IncusFingerprint].Project = "other"
		},
		"missing image": func(client *fakeGatewayStoppedClient, pin *NativeGatewayCreateImage) {
			delete(client.images, pin.Expected.IncusFingerprint)
		},
		"alias retargeted to another local image": func(client *fakeGatewayStoppedClient, pin *NativeGatewayCreateImage) {
			client.aliases[pin.Evidence.Alias].Target = client.imagesForCreate.Postgres.Expected.IncusFingerprint
		},
	} {
		t.Run(name, func(t *testing.T) {
			top, devices, pair, client := stoppedClientFixture(t)
			pins := client.imagesForCreate
			drift(client, &pins.LiteLLM) // Even a valid PostgreSQL must not be created.
			result, err := CreateStoppedGatewayWithClient(context.Background(), top, devices, pair, pins, client,
				func(context.Context) error { t.Fatal("unverified pair reached write guard"); return nil })
			var stoppedErr *NativeGatewayStoppedError
			if !errors.As(err, &stoppedErr) || stoppedErr.Attempted || stoppedErr.Uncertain ||
				len(result.Created)+len(result.Verified)+len(client.created) != 0 {
				t.Fatalf("unexpected outcome: %+v %v; submissions %v", result, err, client.created)
			}
		})
	}
}

func TestCreateStoppedGatewayRechecksPairAtWriteBoundary(t *testing.T) {
	top, devices, pair, client := stoppedClientFixture(t)
	// Retarget the already verified PostgreSQL alias between the two writes.
	client.onCreate = func(req api.InstancesPost) error {
		if req.Name == pair.Postgres.Name {
			client.aliases[pair.Postgres.ImageAlias].Target = client.imagesForCreate.LiteLLM.Expected.IncusFingerprint
		}
		return nil
	}
	result, err := CreateStoppedGatewayWithClient(context.Background(), top, devices, pair, client.imagesForCreate, client, func(context.Context) error { return nil })
	var stoppedErr *NativeGatewayStoppedError
	if !errors.As(err, &stoppedErr) || stoppedErr.Stage != "mutation-boundary-instances" ||
		stoppedErr.Attempted || stoppedErr.Uncertain || !slices.Equal(client.created, []string{pair.Postgres.Name}) ||
		!slices.Equal(result.Created, client.created) || !slices.Equal(result.Verified, client.created) {
		t.Fatalf("drift did not preserve verified partial output: %+v %v; submissions %v", result, err, client.created)
	}
}

func TestCreateStoppedGatewayRejectsExistingWrongBaseImage(t *testing.T) {
	top, devices, pair, client := stoppedClientFixture(t)
	guard := func(context.Context) error { return nil }
	if _, err := CreateStoppedGatewayWithClient(context.Background(), top, devices, pair, client.imagesForCreate, client, guard); err != nil {
		t.Fatal(err)
	}
	client.instances[pair.Postgres.Name].Config["volatile.base_image"] = client.imagesForCreate.LiteLLM.Expected.IncusFingerprint
	result, err := CreateStoppedGatewayWithClient(context.Background(), top, devices, pair, client.imagesForCreate, client, guard)
	if err == nil || len(result.Verified) != 0 || len(client.created) != 2 {
		t.Fatalf("wrong base was retained or replaced: %+v %v; submissions %v", result, err, client.created)
	}
}

func TestCreateStoppedGatewayChecksGuardAtWriteEdge(t *testing.T) {
	for _, cancelAtGuard := range []bool{false, true} {
		name := "authority"
		if cancelAtGuard {
			name = "cancellation"
		}
		t.Run(name, func(t *testing.T) {
			top, devices, pair, client := stoppedClientFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			lost := errors.New("synthetic fence loss")
			reads := 0
			client.onGetInstance = func(name string) {
				if name == pair.LiteLLM.Name {
					reads++
				}
			}
			want := lost
			if cancelAtGuard {
				want = context.Canceled
			}
			result, err := CreateStoppedGatewayWithClient(ctx, top, devices, pair, client.imagesForCreate, client, func(context.Context) error {
				if reads != 2 {
					t.Fatalf("guard ran before final pair observation: %d reads", reads)
				}
				if cancelAtGuard {
					cancel()
					return nil
				}
				return lost
			})
			var stoppedErr *NativeGatewayStoppedError
			if !errors.As(err, &stoppedErr) || !errors.Is(err, want) || stoppedErr.Stage != name ||
				stoppedErr.Attempted || stoppedErr.Uncertain || len(result.Created)+len(result.Verified)+len(client.created) != 0 {
				t.Fatalf("write was not refused: %+v %v; submissions %v", result, err, client.created)
			}
		})
	}
}
