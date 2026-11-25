package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	"github.com/lxc/incus/v7/shared/api"
)

func startFixtureDescriptor(top NativeGatewayTopology) environment.Descriptor {
	desc := gatewayTestEnvironment(top.EnvironmentID, top.Project)
	desc.Binding.ServerID = "sha256:" + strings.Repeat("a", 64)
	return desc
}

type environmentStartClient struct {
	*fakeGatewayStoppedClient
	transitions []string
	server      *api.Server
}

func (c *environmentStartClient) GetServer() (*api.Server, string, error) {
	return c.server, "etag", nil
}
func (c *environmentStartClient) UpdateInstanceState(name string, req api.InstanceStatePut, _ string) (native.InstanceOperation, error) {
	if req.Action != "start" || req.Force {
		return nil, errors.New("unexpected gateway transition")
	}
	c.transitions = append(c.transitions, name)
	c.instances[name].Status = "Running"
	return successfulGatewayCreateOperation{}, nil
}

type envGatewayReadyFunc func(context.Context, NativeGatewayResourceIdentity, string) error

func (f envGatewayReadyFunc) VerifyGatewayServiceReady(ctx context.Context, id NativeGatewayResourceIdentity, role string) error {
	return f(ctx, id, role)
}
func environmentStartFixture(t *testing.T) (NativeGatewayTopology, NativeGatewayDevices, NativeGatewayInstancePair, *environmentStartClient, NativeGatewayRuntimeBinding) {
	t.Helper()
	top, devices, pair, client := environmentGatewayCreateFixture(t)
	if _, err := CreateStoppedGatewayWithClient(context.Background(), top, devices, pair, client.imagesForCreate, client, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	desc := startFixtureDescriptor(top)
	id, err := NativeGatewayIdentity(desc, desc.Binding)
	if err != nil {
		t.Fatal(err)
	}
	b := NativeGatewayRuntimeBinding{Resources: id, AcceptedEnvironmentRevision: fixtureGatewayRevision(t, desc), CredentialVersion: "v1",
		Images: client.imagesForCreate, RootPool: devices.Postgres["root"]["pool"], DatabasePool: devices.Postgres["database"]["pool"]}
	server := &api.Server{Environment: api.ServerEnvironment{Project: desc.Binding.Project, CertificateFingerprint: strings.TrimPrefix(desc.Binding.ServerID, "sha256:")}}
	return top, devices, pair, &environmentStartClient{fakeGatewayStoppedClient: client, server: server}, b
}
func TestEnvironmentGatewayStartOrderedAndIdempotent(t *testing.T) {
	top, devices, pair, client, b := environmentStartFixture(t)
	desc := startFixtureDescriptor(top)
	roles := []string{}
	probe := envGatewayReadyFunc(func(_ context.Context, id NativeGatewayResourceIdentity, role string) error {
		if id != b.Resources {
			t.Fatal("readiness target mismatch")
		}
		roles = append(roles, role)
		return nil
	})
	verify := gatewayDeliveryFunc(func(context.Context, NativeGatewayRuntimeBinding) error { return nil })
	result, err := StartNativeEnvironmentGatewayWithClient(context.Background(), desc, desc.Binding,
		top, devices, pair, b, client, func(context.Context) error { return nil }, verify, probe)
	if err != nil || len(result.Started) != 2 || len(result.Ready) != 2 ||
		result.Started[0] != pair.Postgres.Name || result.Started[1] != pair.LiteLLM.Name ||
		len(roles) != 2 || roles[0] != "postgres" || roles[1] != "litellm" {
		t.Fatalf("gateway startup order: %+v %v roles=%v", result, err, roles)
	}
	repeat, err := StartNativeEnvironmentGatewayWithClient(context.Background(), desc, desc.Binding,
		top, devices, pair, b, client, func(context.Context) error { return nil }, verify, probe)
	if err != nil || len(repeat.Started) != 0 || len(repeat.Ready) != 2 || len(client.transitions) != 2 {
		t.Fatalf("idempotent gateway start: %+v %v", repeat, err)
	}
}
func TestEnvironmentGatewayStartStopsAtUnreadyPostgres(t *testing.T) {
	top, devices, pair, client, b := environmentStartFixture(t)
	desc := startFixtureDescriptor(top)
	verify := gatewayDeliveryFunc(func(context.Context, NativeGatewayRuntimeBinding) error { return nil })
	probe := envGatewayReadyFunc(func(_ context.Context, _ NativeGatewayResourceIdentity, role string) error {
		if role != "postgres" {
			t.Fatal("LiteLLM readiness must not run")
		}
		return errors.New("synthetic-not-ready")
	})
	result, err := StartNativeEnvironmentGatewayWithClient(context.Background(), desc, desc.Binding,
		top, devices, pair, b, client, func(context.Context) error { return nil }, verify, probe)
	var startupErr *NativeEnvironmentGatewayStartError
	if !errors.As(err, &startupErr) || !startupErr.Attempted || startupErr.Uncertain || len(result.Started) != 1 || len(result.Ready) != 0 ||
		client.instances[pair.LiteLLM.Name].Status != "Stopped" {
		t.Fatalf("unready PostgreSQL did not stop progression: %+v %v", result, err)
	}
}

func TestEnvironmentGatewayStartRejectsUnpinnedOrUnsafeResources(t *testing.T) {
	for _, tc := range []string{"server", "config-pin", "raw-config", "secret-config", "revision"} {
		t.Run(tc, func(t *testing.T) {
			top, devices, pair, client, b := environmentStartFixture(t)
			desc := startFixtureDescriptor(top)
			const secret = "synthetic-secret-must-not-escape"
			switch tc {
			case "server":
				client.server.Environment.CertificateFingerprint = strings.Repeat("f", 64)
			case "config-pin":
				b.Images.LiteLLM.Expected.ConfigDigest = "sha256:" + strings.Repeat("f", 64)
			case "raw-config":
				client.instances[pair.LiteLLM.Name].ExpandedConfig = map[string]string{"raw.lxc": secret}
			case "secret-config":
				client.instances[pair.LiteLLM.Name].Config["environment.DATABASE_URL"] = secret
			case "revision":
				b.AcceptedEnvironmentRevision.ID = strings.Repeat("f", 64)
			}
			unexpected := func(context.Context) error { t.Fatal("invalid gateway reached authority/probe"); return nil }
			_, err := StartNativeEnvironmentGatewayWithClient(context.Background(), desc, desc.Binding, top, devices, pair, b, client, unexpected,
				gatewayDeliveryFunc(func(ctx context.Context, _ NativeGatewayRuntimeBinding) error { return unexpected(ctx) }),
				envGatewayReadyFunc(func(ctx context.Context, _ NativeGatewayResourceIdentity, _ string) error { return unexpected(ctx) }))
			if err == nil || strings.Contains(err.Error(), secret) || len(client.transitions) != 0 {
				t.Fatalf("unsafe gateway accepted or leaked diagnostic: %v", err)
			}
		})
	}
}
func TestEnvironmentGatewayStartRejectsForeignOwnerOrMissingAuthority(t *testing.T) {
	top, devices, pair, client, b := environmentStartFixture(t)
	desc := startFixtureDescriptor(top)
	verify := gatewayDeliveryFunc(func(context.Context, NativeGatewayRuntimeBinding) error { return nil })
	probe := envGatewayReadyFunc(func(context.Context, NativeGatewayResourceIdentity, string) error { return nil })
	if _, err := StartNativeEnvironmentGatewayWithClient(context.Background(), desc, desc.Binding,
		top, devices, pair, b, client, nil, verify, probe); err == nil || len(client.transitions) != 0 {
		t.Fatal("nil lease accepted")
	}
	client.instances[pair.Postgres.Name].Config["user.aginctus.environment-id"] = "123e4567-e89b-42d3-a456-426614174001"
	if _, err := StartNativeEnvironmentGatewayWithClient(context.Background(), desc, desc.Binding,
		top, devices, pair, b, client, func(context.Context) error { return nil }, verify, probe); err == nil ||
		len(client.transitions) != 0 {
		t.Fatal("foreign environment guest started")
	}
}

func TestEnvironmentGatewayStartBlocksNextGuestWhenEvidenceBecomesStale(t *testing.T) {
	for _, tc := range []string{"revoked", "cancelled", "postgres stopped"} {
		t.Run(tc, func(t *testing.T) {
			top, devices, pair, client, b := environmentStartFixture(t)
			desc := startFixtureDescriptor(top)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			revoked := false
			verify := gatewayDeliveryFunc(func(context.Context, NativeGatewayRuntimeBinding) error {
				if revoked {
					return errors.New("synthetic secret diagnostic")
				}
				return nil
			})
			ready := envGatewayReadyFunc(func(_ context.Context, _ NativeGatewayResourceIdentity, role string) error {
				if role != "postgres" {
					t.Fatal("unexpected LiteLLM probe")
				}
				switch tc {
				case "revoked":
					revoked = true
				case "cancelled":
					cancel()
				case "postgres stopped":
					client.instances[pair.Postgres.Name].Status = "Stopped"
				}
				return nil
			})
			got, err := StartNativeEnvironmentGatewayWithClient(ctx, desc, desc.Binding, top, devices, pair, b, client, context.Context.Err, verify, ready)
			if err == nil || strings.Contains(err.Error(), "synthetic secret diagnostic") || len(got.Started) != 1 || len(got.Ready) != 0 || len(client.transitions) != 1 {
				t.Fatalf("stale evidence allowed progress: %+v %v", got, err)
			}
		})
	}
}
