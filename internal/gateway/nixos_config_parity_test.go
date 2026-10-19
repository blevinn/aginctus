package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
)

// The flake check supplies actual environment.etc sources. The snapshot keeps
// the same contract executable in plain Go tests without requiring Nix.
func TestNixOSGatewayConfigParity(t *testing.T) {
	readFixtures := func(path string) map[string]json.RawMessage {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var fixtures map[string]json.RawMessage
		if err := json.Unmarshal(data, &fixtures); err != nil {
			t.Fatal(err)
		}
		return fixtures
	}
	fixtures := readFixtures("testdata/nixos-gateway.json")
	if path := os.Getenv("AGINCTUS_NIXOS_GATEWAY_FIXTURES"); path != "" {
		generated := readFixtures(path)
		var snapshotModel, generatedModel any
		snapshotJSON, _ := json.Marshal(fixtures)
		generatedJSON, _ := json.Marshal(generated)
		if err := json.Unmarshal(snapshotJSON, &snapshotModel); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(generatedJSON, &generatedModel); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(snapshotModel, generatedModel) {
			t.Fatal("NixOS config sources differ from testdata/nixos-gateway.json; regenerate and review the snapshot")
		}
		fixtures = generated
	}
	if len(fixtures) != 4 {
		t.Fatalf("fixture count = %d, want 4", len(fixtures))
	}
	for _, name := range []string{"defaults", "disabled", "ranges", "static"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, fixtures[name], 0o600); err != nil {
				t.Fatal(err)
			}
			// Load as the system document without ambient user files or env.
			effective, err := (&config.Loader{SystemPath: path}).Load(config.Options{})
			if err != nil {
				t.Fatal(err)
			}
			spec, err := FromConfig(effective)
			if err != nil {
				t.Fatal(err)
			}
			plan, compose := assertRenderedGatewayComposeMatchesDeploymentModel(t, spec)
			wantSteps := []string{"management-network", "gateway-egress-network", "gateway"}
			if name == "disabled" {
				wantSteps = []string{"management-network", "gateway"}
			}
			var gotSteps []string
			for _, step := range plan.Steps {
				gotSteps = append(gotSteps, step.ID)
			}
			if !reflect.DeepEqual(gotSteps, wantSteps) {
				t.Fatalf("step order = %v, want %v", gotSteps, wantSteps)
			}
			managementAddress := "auto"
			if name == "ranges" || name == "static" {
				managementAddress = "10.42.0.1/24"
			}
			assertGatewayNetworkDocument(t, plan.Steps[0].Configuration, "aginctus-mgmt", map[string]string{
				"ipv4.address": managementAddress, "ipv4.nat": "false", "ipv4.routing": "false", "ipv6.address": "none",
				"user.aginctus.managed": "true", "user.aginctus.resource": "management-network",
			})
			services := compose["services"].(map[string]any)
			postgres := services["postgres"].(map[string]any)
			litellm := services["litellm"].(map[string]any)
			if got := postgres["networks"]; !reflect.DeepEqual(got, []any{"management"}) {
				t.Fatalf("PostgreSQL attachments = %#v", got)
			}
			networks := compose["networks"].(map[string]any)
			wantNetworks := map[string]any{
				"management": map[string]any{"external": true, "name": "aginctus-mgmt"},
			}
			var wantAttachments any = []any{"management"}
			if name != "disabled" {
				bridge, nic := "auto", ""
				if name == "ranges" || name == "static" {
					bridge, nic = "10.42.40.1/24", "10.42.40.25/24"
				}
				wantConfig := map[string]string{
					"ipv4.address": bridge, "ipv4.nat": "true", "ipv4.routing": "true", "ipv6.address": "none",
					"user.aginctus.managed": "true", "user.aginctus.resource": "gateway-egress-network",
				}
				if name == "ranges" {
					wantConfig["ipv4.dhcp.ranges"] = "10.42.40.100-10.42.40.199"
				}
				if name == "static" {
					wantConfig["ipv4.dhcp"] = "false"
				}
				assertGatewayNetworkDocument(t, plan.Steps[1].Configuration, "aginctus-egress", wantConfig)
				egress := map[string]any{"external": true, "name": "aginctus-egress"}
				egressNIC := map[string]any{}
				if nic != "" {
					egress["x-incus"] = map[string]any{"ipv4.address": bridge}
					egressNIC["ipv4_address"] = nic
				}
				wantNetworks["egress"] = egress
				// internal suppresses the management default gateway; the
				// external egress CIDR supplies gateway metadata to Compose.
				wantAttachments = map[string]any{
					"management": map[string]any{"x-incus-compose": map[string]any{"internal": true}},
					"egress":     egressNIC,
				}
			}
			if !reflect.DeepEqual(networks, wantNetworks) {
				t.Fatalf("Compose networks = %#v, want %#v", networks, wantNetworks)
			}
			if got := litellm["networks"]; !reflect.DeepEqual(got, wantAttachments) {
				t.Fatalf("LiteLLM attachments = %#v, want %#v", got, wantAttachments)
			}
		})
	}
}

func assertGatewayNetworkDocument(t *testing.T, raw []byte, name string, wantConfig map[string]string) {
	t.Helper()
	var step struct {
		Operation                string            `json:"operation"`
		RejectUnsupportedChanges bool              `json:"rejectUnsupportedChanges"`
		RequireExistingConfig    map[string]string `json:"requireExistingConfig"`
		Documents                []struct {
			Kind        string            `json:"kind"`
			Name        string            `json:"name"`
			NetworkType string            `json:"networkType"`
			Config      map[string]string `json:"config"`
		} `json:"documents"`
	}
	if err := json.Unmarshal(raw, &step); err != nil {
		t.Fatal(err)
	}
	if step.Operation != "upsert" || !step.RejectUnsupportedChanges || !reflect.DeepEqual(step.RequireExistingConfig, map[string]string{
		"user.aginctus.managed": "true", "user.aginctus.resource": wantConfig["user.aginctus.resource"],
	}) {
		t.Fatalf("%s lost ownership/change guards: %s", name, raw)
	}
	if len(step.Documents) != 1 {
		t.Fatalf("%s documents = %d, want 1", name, len(step.Documents))
	}
	doc := step.Documents[0]
	if doc.Kind != "network" || doc.Name != name || doc.NetworkType != "bridge" || !reflect.DeepEqual(doc.Config, wantConfig) {
		t.Fatalf("%s network = %+v, want config %#v", name, doc, wantConfig)
	}
}
