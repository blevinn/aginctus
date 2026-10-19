package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/orchestration"
)

func TestApplyRenderEgressDHCP(t *testing.T) {
	const pool = "10.42.40.100-10.42.40.199"
	for _, tt := range []struct {
		name    string
		members string
		dhcp    string
		ranges  string
		wantErr string
	}{
		{name: "legacy enabled", members: `"dhcp":true`},
		{name: "legacy disabled", members: `"dhcp":false`, dhcp: "false"},
		{name: "nested enabled", members: `"dhcp":{"enabled":true,"ranges":"` + pool + `"}`, ranges: pool},
		{name: "nested disabled", members: `"dhcp":{"enabled":false}`, dhcp: "false"},
		{name: "mistyped enabled", members: `"dhcp":{"enabled":"sensitive-input"}`, wantErr: "enabled must be a boolean"},
		{name: "dotted sibling", members: `"dhcp":true,"dhcp.ranges":"sensitive-input"`, wantErr: "literal dotted"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			body := fmt.Sprintf(`{"gateway":{"egress":{
				"ipv4":{"address":"10.42.40.25/24"},
				"network":{"ipv4":{"address":"10.42.40.1/24",%s}}
			}}}`, tt.members)
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			args := []string{"--configuration-file=" + path, "apply", "--render"}
			code := Run(context.Background(), args, &out, &stderr, &fakeIncusClient{}, &config.Loader{})
			if tt.wantErr != "" {
				if code != 1 || out.Len() != 0 || !strings.Contains(stderr.String(), tt.wantErr) || strings.Contains(stderr.String(), "sensitive-input") {
					t.Fatalf("invalid policy: code=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
				}
				return
			}
			if code != 0 {
				t.Fatalf("render code=%d stderr=%q", code, stderr.String())
			}
			var plan orchestration.Plan
			if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
				t.Fatal(err)
			}
			for _, step := range plan.Steps {
				if step.ID != "gateway-egress-network" {
					continue
				}
				var rendered struct {
					Documents []struct {
						Config map[string]string `json:"config"`
					} `json:"documents"`
				}
				if err := json.Unmarshal(step.Configuration, &rendered); err != nil {
					t.Fatal(err)
				}
				if len(rendered.Documents) != 1 {
					t.Fatalf("egress documents = %d, want 1", len(rendered.Documents))
				}
				settings := rendered.Documents[0].Config
				for key, want := range map[string]string{"ipv4.dhcp": tt.dhcp, "ipv4.dhcp.ranges": tt.ranges} {
					got, present := settings[key]
					if got != want || present != (want != "") {
						t.Errorf("literal Incus key %q = %q (present=%v), want %q", key, got, present, want)
					}
				}
				return
			}
			t.Fatal("render omitted gateway-egress-network")
		})
	}
}
