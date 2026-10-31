// Package nativeoci contains disposable feasibility tests, not a production driver.
package nativeoci

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	agincus "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration/native"
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
	if err := server.CreateProject(api.ProjectsPost{
		Name: scope.Name, ProjectPut: api.ProjectPut{Config: scope.DesiredConfig()},
	}); err != nil {
		t.Fatal(err)
	}
	client := server.UseProject(scope.Name)
	if err := server.UseProject("default").CreateNetwork(api.NetworksPost{Name: "spike-mgmt", Type: "bridge", NetworkPut: api.NetworkPut{Config: map[string]string{
		"ipv4.address": "10.78.0.1/24", "ipv4.nat": "false", "ipv4.routing": "false", "ipv4.dhcp.gateway": "none", "ipv6.address": "none",
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := client.CreateStoragePoolVolume("spike", api.StorageVolumesPost{Name: "postgres-data", Type: "custom", StorageVolumePut: api.StorageVolumePut{Config: map[string]string{"security.shifted": "true"}}}); err != nil {
		t.Fatal(err)
	}
	for _, image := range []struct{ name, registry, reference string }{
		{"postgres", "https://docker.io", "library/postgres:17-alpine"},
		{"litellm", "https://ghcr.io", "berriai/litellm:v1.103.0"},
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
			request := api.ImagesPost{Source: &api.ImagesPostSource{
				Type: "image", Mode: "pull", ImageSource: api.ImageSource{
					Protocol: "oci", Server: image.registry, Alias: image.reference,
					ImageType: "container",
				},
			}}
			op, err := client.CreateImage(request, nil)
			wait(op, err)
			fingerprint, _ := op.Get().Metadata["fingerprint"].(string)
			if len(fingerprint) != 64 {
				t.Fatal("missing full OCI fingerprint")
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
			if err := client.CreateImageAlias(api.ImageAliasesPost{ImageAliasesEntry: api.ImageAliasesEntry{Name: alias, ImageAliasesEntryPut: api.ImageAliasesEntryPut{Target: fingerprint}}}); err != nil {
				t.Fatal(err)
			}
			config := map[string]string{
				"environment.AGINCTUS_SPIKE": "synthetic", "user.aginctus.managed": "true",
				"user.aginctus.resource": "workload", "user.aginctus.role": "agent-workload", "user.aginctus.workload": image.name,
			}
			if image.name == "postgres" {
				config["environment.POSTGRES_PASSWORD"] = "disposable-synthetic-password"
			}
			devices := map[string]map[string]string{
				"root": {"type": "disk", "path": "/", "pool": "spike"},
				"eth0": {"type": "nic", "name": "eth0", "network": "spike-mgmt", "ipv4.gateway": "none"},
			}
			if image.name == "postgres" {
				devices["data"] = map[string]string{"type": "disk", "pool": "spike", "source": "postgres-data", "path": "/var/lib/postgresql/data"}
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
			wait(client.UpdateInstanceState(name, api.InstanceStatePut{Action: "start", Timeout: 60}, ""))
			state, _, err := client.GetInstanceState(name)
			if err != nil || state.StatusCode != api.Running {
				t.Fatal("OCI service did not remain running")
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
				probe = []string{"pg_isready", "-U", "postgres"}
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
				if err := execOCI(ctx, client, name, []string{"psql", "-U", "postgres", "-c", "CREATE TABLE aginctus_spike AS SELECT 42 AS value"}); err != nil {
					t.Fatal(err)
				}
			}
			wait(client.UpdateInstanceState(name, api.InstanceStatePut{Action: "stop", Timeout: 60}, ""))
			wait(client.DeleteInstance(name))
			if image.name == "postgres" {
				if _, _, err := client.GetStoragePoolVolume("spike", "custom", "postgres-data"); err != nil {
					t.Fatal("instance removal lost custom volume")
				}
				wait(client.CreateInstance(instance))
				wait(client.UpdateInstanceState(name, api.InstanceStatePut{Action: "start", Timeout: 60}, ""))
				for {
					err := execOCI(readyCtx, client, name, []string{"psql", "-U", "postgres", "-v", "ON_ERROR_STOP=1", "-c", "DO $$ BEGIN IF (SELECT value FROM aginctus_spike) IS DISTINCT FROM 42 THEN RAISE EXCEPTION 'data mismatch'; END IF; END $$"})
					if err == nil {
						break
					}
					select {
					case <-readyCtx.Done():
						t.Fatal("database contents not recovered after recreation")
					case <-time.After(time.Second):
					}
				}
				wait(client.UpdateInstanceState(name, api.InstanceStatePut{Action: "stop", Timeout: 60}, ""))
				wait(client.DeleteInstance(name))
			}
		})
	}
	images, err := server.UseProject("default").GetImages()
	if err != nil || len(images) != 0 {
		t.Fatal("OCI import leaked into the default project")
	}
}

func execOCI(ctx context.Context, client incus.InstanceServer, name string, command []string) error {
	done := make(chan bool)
	op, err := client.ExecInstance(name, api.InstanceExecPost{Command: command, WaitForWS: true}, &incus.InstanceExecArgs{Stdout: io.Discard, Stderr: io.Discard, DataDone: done})
	if err != nil {
		return err
	}
	if err := op.WaitContext(ctx); err != nil {
		return err
	}
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	if code, ok := op.Get().Metadata["return"].(float64); !ok || code != 0 {
		return fmt.Errorf("synthetic application probe failed")
	}
	return nil
}
