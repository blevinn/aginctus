// This external test package prevents a nativeoci -> gateway -> nativeoci
// import cycle and runs only in the explicitly disposable OCI VM.
package nativeoci_test

import (
	"context"
	"maps"
	"os"
	"strings"
	"testing"
	"time"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	"github.com/blevinn/aginctus/internal/gateway"
	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

func gatewayVolumeVMEnvironment(project, fingerprint, id string) environment.Descriptor {
	return environment.Descriptor{ID: id, Name: "disposable-gateway",
		Binding: environment.Binding{Project: project, ServerID: "sha256:" + fingerprint}}
}

func TestNativeGatewayAttachmentAuditVM(t *testing.T) {
	if os.Getenv("AGINCTUS_NATIVE_OCI_SPIKE") != "disposable-vm" {
		t.Skip("requires explicit native OCI disposable-VM opt-in")
	}
	marker, err := os.ReadFile("/run/aginctus-native-oci-disposable")
	if err != nil || string(marker) != "native-oci-vm\n" {
		t.Fatal("disposable VM marker absent")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := incusclient.ConnectIncusUnixWithContext(ctx, "/var/lib/incus/unix.socket", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Disconnect()
	server, _, err := admin.GetServer()
	if err != nil || server.Config["core.https_address"] != "" {
		t.Fatal("test requires local-only Incus daemon")
	}
	scope, err := incusscope.NewProjectScope("aginctus-native-gateway-audit")
	if err != nil {
		t.Fatal(err)
	}
	result, err := native.EnsureLocalProject(ctx, scope, native.ProjectCreateAuthorization{Confirmed: true})
	if err != nil || !result.Verified {
		t.Fatalf("isolated project: %+v %v", result, err)
	}
	for _, n := range []struct {
		name     string
		resource string
		cidr     string
		nat      string
		route    string
	}{
		{"ag-aud-mgmt", "management-network", "10.85.0.1/24", "false", "false"},
		{"ag-aud-egress", "gateway-egress-network", "10.86.0.1/24", "true", "true"},
	} {
		err := admin.UseProject("default").CreateNetwork(api.NetworksPost{
			Name: n.name, Type: "bridge", NetworkPut: api.NetworkPut{Config: map[string]string{
				"ipv4.address": n.cidr, "ipv4.nat": n.nat, "ipv4.routing": n.route,
				"ipv6.address":          "none",
				"user.aginctus.managed": "true", "user.aginctus.resource": n.resource,
			}},
		})
		if err != nil {
			t.Fatalf("create disposable bridge: %v", err)
		}
	}
	client := admin.UseProject(scope.Name)
	descriptor := gatewayVolumeVMEnvironment(scope.Name, server.Environment.CertificateFingerprint, "123e4567-e89b-42d3-a456-426614174000")
	guardCalls := 0
	guard := func(context.Context) error { guardCalls++; return nil } // This VM fixture is the sole writer.
	for attempt := range 2 {
		got, err := gateway.EnsureNativeEnvironmentGatewayVolumeWithClient(ctx, descriptor, descriptor.Binding, "spike", client, guard)
		if err != nil || !got.Verified || got.Created != (attempt == 0) || guardCalls != 1 {
			t.Fatalf("environment volume create/reuse: %+v %v (guards=%d)", got, err, guardCalls)
		}
	}
	top := gateway.NativeGatewayTopology{
		Deployment: "gateway", Project: scope.Name,
		ManagementNetwork: "ag-aud-mgmt", EgressNetwork: "ag-aud-egress",
		DatabaseVolume: "gateway-postgres",
		Services: []gateway.NativeGatewayService{
			{Name: "postgres", ImageReference: "docker.io/library/postgres@sha256:" + strings.Repeat("a", 64),
				Networks:          []gateway.NativeGatewayNetwork{{Name: "ag-aud-mgmt"}},
				Volume:            "gateway-postgres",
				SecretEnvironment: []string{"AGINCTUS_GATEWAY_POSTGRES_PASSWORD"}},
			{Name: "litellm", ImageReference: "ghcr.io/berriai/litellm@sha256:" + strings.Repeat("b", 64),
				Networks: []gateway.NativeGatewayNetwork{
					{Name: "ag-aud-mgmt"},
					{Name: "ag-aud-egress", IPv4Address: "10.86.0.12/24"},
				}, DependsOn: []string{"postgres"},
				SecretEnvironment: []string{
					"AGINCTUS_GATEWAY_POSTGRES_PASSWORD", "AGINCTUS_GATEWAY_MASTER_KEY", "AGINCTUS_GATEWAY_SALT_KEY",
				}},
		},
	}
	unboundTopology := top
	top, err = gateway.BindNativeGatewayTopology(unboundTopology, descriptor, descriptor.Binding)
	if err != nil {
		t.Fatal(err)
	}
	devices, err := gateway.PlanNativeGatewayDevices(top, "spike", "spike")
	if err != nil {
		t.Fatal(err)
	}
	if err := gateway.ObserveLocalNativeGatewayAttachments(ctx, top, devices); err != nil {
		t.Fatalf("real Incus native gateway attachment preflight: %v", err)
	}
	before, beforeETag, err := client.GetStoragePoolVolume("spike", "custom", top.DatabaseVolume)
	if err != nil {
		t.Fatal(err)
	}
	for _, collision := range []struct{ id, owner string }{
		{"123e4567-e89b-42d3-a456-426614174001", ""},
		{"123e4567-e89b-42d3-a456-426614174002", descriptor.ID},
	} {
		sibling := gatewayVolumeVMEnvironment(scope.Name, server.Environment.CertificateFingerprint, collision.id)
		identity, err := gateway.NativeGatewayIdentity(sibling, sibling.Binding)
		if err != nil {
			t.Fatal(err)
		}
		config := maps.Clone(before.Config)
		delete(config, "user.aginctus.environment-id")
		if collision.owner != "" {
			config["user.aginctus.environment-id"] = collision.owner
		}
		if err := client.CreateStoragePoolVolume("spike", api.StorageVolumesPost{
			Name: identity.DatabaseVolume, Type: "custom", ContentType: "filesystem",
			StorageVolumePut: api.StorageVolumePut{Config: config},
		}); err != nil {
			t.Fatal(err)
		}
		if got, err := gateway.EnsureNativeEnvironmentGatewayVolumeWithClient(ctx, sibling, sibling.Binding, "spike", client, guard); err == nil || got.Verified || got.Created || guardCalls != 1 {
			t.Fatalf("ownership collision accepted or attempted: %+v %v (guards=%d)", got, err, guardCalls)
		}
		siblingTopology, err := gateway.BindNativeGatewayTopology(unboundTopology, sibling, sibling.Binding)
		if err != nil {
			t.Fatal(err)
		}
		siblingDevices, err := gateway.PlanNativeGatewayDevices(siblingTopology, "spike", "spike")
		if err != nil {
			t.Fatal(err)
		}
		if err := gateway.ObserveLocalNativeGatewayAttachments(ctx, siblingTopology, siblingDevices); err == nil {
			t.Fatal("attachment audit accepted a volume with missing or foreign ownership")
		}
		retained, _, err := client.GetStoragePoolVolume("spike", "custom", identity.DatabaseVolume)
		if err != nil || !maps.Equal(retained.Config, config) {
			t.Fatalf("collision changed: %v", err)
		}
	}
	after, afterETag, err := client.GetStoragePoolVolume("spike", "custom", top.DatabaseVolume)
	if err != nil || beforeETag != afterETag || !maps.Equal(before.Config, after.Config) {
		t.Fatalf("selected environment volume changed while reconciling siblings: %v", err)
	}
	// The same selected project must reject an unowned or missing bridge;
	// no mutation is attempted by the audit.
	top.EgressNetwork = "aginctus-foreign"
	top.Services[1].Networks[1].Name = top.EgressNetwork
	devices, err = gateway.PlanNativeGatewayDevices(top, "spike", "spike")
	if err != nil {
		t.Fatal(err)
	}
	if err := gateway.ObserveLocalNativeGatewayAttachments(ctx, top, devices); err == nil {
		t.Fatal("missing/foreign egress bridge passed real-Incus preflight")
	}
}
