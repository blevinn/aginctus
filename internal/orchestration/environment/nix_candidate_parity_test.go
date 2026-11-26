package environment

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
)

func TestNixEnvironmentCandidateParity(t *testing.T) {
	manifest := os.Getenv("AGINCTUS_ENVIRONMENT_CANDIDATE_MANIFEST")
	if manifest == "" {
		t.Skip("run through the environment-candidate-parity flake check")
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var sources map[string]struct{ NixOS, Home string }
	if err := json.Unmarshal(data, &sources); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"old", "native", "noEgress"} {
		t.Run(name, func(t *testing.T) {
			file := sources[name]
			read := func(path string) []byte {
				t.Helper()
				b, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				return b
			}
			nixos, home := read(file.NixOS), read(file.Home)
			if !bytes.Equal(nixos, home) {
				t.Fatal("candidate emitters disagree")
			}
			c, err := ParseCandidate(nixos)
			if err != nil {
				t.Fatal(err)
			}
			if c.Environment.Binding != (Binding{Project: "candidate-project", ServerID: "sha256:" + strings.Repeat("a", 64)}) ||
				c.Workloads[0].Native != nil || !slices.Equal(c.Workloads[0].Profiles, []string{"second", "first"}) {
				t.Fatal("candidate changed explicit scope, omitted native intent, or profile order")
			}
			if name == "old" {
				if len(c.Workloads) != 1 || c.Herdr != nil || c.Gateway != nil {
					t.Fatal("legacy-shaped environment candidate gained implicit lifecycle intent")
				}
				return
			}
			wantNative := &acceptedstate.WorkloadNativeSpec{Runtime: "opencode", StoragePool: "workload-pool", ManagementNetwork: "approved-mgmt"}
			wantHerdr := &HerdrSpec{InstanceName: "herdr-client", ImageFingerprint: strings.Repeat("f", 64), StoragePool: "herdr-pool", ManagementNetwork: "approved-mgmt", AllowStateBootstrap: false}
			if len(c.Workloads) != 2 || c.Workloads[1].InstanceType != "virtual-machine" || !c.Workloads[1].EnsureRunning ||
				!reflect.DeepEqual(c.Workloads[1].Native, wantNative) || !reflect.DeepEqual(c.Herdr, wantHerdr) {
				t.Fatal("native workload or Herdr fields did not round trip")
			}
			pg := GatewayImageSpec{Registry: "registry.example.org:5443", Repository: "mirror/postgres", Architecture: "arm64", Variant: "v8", ManifestDigest: "sha256:" + strings.Repeat("c", 64), ConfigDigest: "sha256:" + strings.Repeat("d", 64), IncusFingerprint: strings.Repeat("e", 64), Alias: "pg-pinned"}
			llm := pg
			llm.Registry, llm.Repository, llm.Alias, llm.Variant = "ghcr.io", "mirror/litellm", "llm-pinned", ""
			wantGateway := &GatewaySpec{ManagementNetwork: "approved-mgmt", EgressNetwork: "approved-egress", EgressIPv4: "10.42.40.25/24", RootPool: "gateway-pool", DatabasePool: "database-pool", Postgres: pg, LiteLLM: llm, CredentialVersion: "revision-3", EnsureRunning: true}
			if name == "noEgress" {
				wantGateway.EgressNetwork, wantGateway.EgressIPv4 = "", ""
			}
			if !reflect.DeepEqual(c.Gateway, wantGateway) {
				t.Fatal("gateway fields did not round trip")
			}
			revision, err := BuildAcceptedRevision(nil, nil, c)
			if err != nil || revision.ValidateAgainst(c) != nil {
				t.Fatalf("emitted candidate is not digestible by the accepted revision contract: %v", err)
			}
		})
	}
}
