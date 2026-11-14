// This external test package prevents a nativeoci -> gateway -> nativeoci
// import cycle and runs only in the explicitly disposable OCI VM.
package nativeoci_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	"github.com/blevinn/aginctus/internal/gateway"
	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

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
		{"aginctus-audit-mgmt", "management-network", "10.85.0.1/24", "false", "false"},
		{"aginctus-audit-egress", "gateway-egress-network", "10.86.0.1/24", "true", "true"},
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
	err = admin.UseProject(scope.Name).CreateStoragePoolVolume("spike", api.StorageVolumesPost{
		Name: "gateway-postgres", Type: "custom", ContentType: "filesystem",
		StorageVolumePut: api.StorageVolumePut{Config: map[string]string{
			"user.aginctus.managed":    "true",
			"user.aginctus.resource":   "gateway-postgres-volume",
			"user.aginctus.deployment": scope.Name,
			"security.shifted":         "true",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	top := gateway.NativeGatewayTopology{
		Deployment: "gateway", Project: scope.Name,
		ManagementNetwork: "aginctus-audit-mgmt", EgressNetwork: "aginctus-audit-egress",
		DatabaseVolume: "gateway-postgres",
		Services: []gateway.NativeGatewayService{
			{Name: "postgres", ImageReference: "docker.io/library/postgres@sha256:" + strings.Repeat("a", 64),
				Networks:          []gateway.NativeGatewayNetwork{{Name: "aginctus-audit-mgmt"}},
				Volume:            "gateway-postgres",
				SecretEnvironment: []string{"AGINCTUS_GATEWAY_POSTGRES_PASSWORD"}},
			{Name: "litellm", ImageReference: "ghcr.io/berriai/litellm@sha256:" + strings.Repeat("b", 64),
				Networks: []gateway.NativeGatewayNetwork{
					{Name: "aginctus-audit-mgmt"},
					{Name: "aginctus-audit-egress", IPv4Address: "10.86.0.12/24"},
				}, DependsOn: []string{"postgres"},
				SecretEnvironment: []string{
					"AGINCTUS_GATEWAY_POSTGRES_PASSWORD", "AGINCTUS_GATEWAY_MASTER_KEY", "AGINCTUS_GATEWAY_SALT_KEY",
				}},
		},
	}
	devices, err := gateway.PlanNativeGatewayDevices(top, "spike", "spike")
	if err != nil {
		t.Fatal(err)
	}
	if err := gateway.ObserveLocalNativeGatewayAttachments(ctx, top, devices); err != nil {
		t.Fatalf("real Incus native gateway attachment preflight: %v", err)
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
