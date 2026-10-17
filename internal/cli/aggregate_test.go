package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/orchestration"
)

func TestAggregateApplyRenderDeterministicSharedNetwork(t *testing.T) {
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	args := []string{"apply", "--render"}
	var first []byte
	for attempt := 0; attempt < 2; attempt++ {
		var out, errOut bytes.Buffer
		code := Run(context.Background(), args, &out, &errOut, &fakeIncusClient{}, loader)
		if code != 0 {
			t.Fatalf("render code = %d, stderr = %q", code, errOut.String())
		}
		if attempt == 0 {
			first = append([]byte(nil), out.Bytes()...)
		} else if !bytes.Equal(first, out.Bytes()) {
			t.Fatal("render output is not deterministic")
		}
	}
	var plan orchestration.Plan
	if err := json.Unmarshal(first, &plan); err != nil {
		t.Fatal(err)
	}
	if err := plan.Validate(); err != nil {
		t.Fatal(err)
	}
	expected := []string{"management-network", "herdr-system-state", "herdr-client", "gateway-egress-network", "gateway", "workload-dev"}
	if len(plan.Steps) != len(expected) {
		t.Fatalf("steps = %d, want %d: %s", len(plan.Steps), len(expected), first)
	}
	for i, want := range expected {
		if plan.Steps[i].ID != want {
			t.Fatalf("step %d = %q, want %q", i, plan.Steps[i].ID, want)
		}
	}
}

func TestAggregateApplyRejectsInvalidArguments(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"apply", "--invalid"}, &out, &errOut, &fakeIncusClient{}, &fakeConfigLoader{})
	if code != 2 || !strings.Contains(errOut.String(), "Usage:") {
		t.Fatalf("code = %d, stderr = %q", code, errOut.String())
	}
}

func TestAggregateApplyFromRejectsMalformedFileWithoutExecution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(path, []byte(`{"apiVersion":"wrong"}`), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"apply", "--from", path}, &out, &errOut, &fakeIncusClient{}, &fakeConfigLoader{})
	if code != 1 || !strings.Contains(errOut.String(), "orchestration") {
		t.Fatalf("code = %d, stderr = %q", code, errOut.String())
	}
}

func TestAggregateApplyFromRejectsNonRegularFile(t *testing.T) {
	if _, err := readAggregatePlan(t.TempDir()); err == nil {
		t.Fatal("expected directory rejection")
	}
}

func TestAggregateApplyFromRejectsTrailingDocuments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(path, []byte("{}\\n{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readAggregatePlan(path); err == nil {
		t.Fatal("expected multi-document rejection")
	}
}

func TestAggregateReadMultiDocumentYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.yaml")
	data := `apiVersion: aginctus.dev/v1alpha1
kind: Orchestration
metadata:
  name: aginctus
steps:
  - id: first
    driver: apply
    configuration: {}
---
apiVersion: aginctus.dev/v1alpha1
kind: Orchestration
metadata:
  name: aginctus
steps:
  - id: second
    driver: apply
    configuration: {}
`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := readAggregatePlan(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Steps) != 2 || p.Steps[0].ID != "first" || p.Steps[1].ID != "second" {
		t.Fatalf("steps=%v", p.Steps)
	}
}

func TestAggregateRenderStreamRoundTrip(t *testing.T) {
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	var out, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"apply", "--render"}, &out, &stderr, &fakeIncusClient{}, loader); code != 0 {
		t.Fatalf("render: %s", stderr.String())
	}
	p, err := orchestration.ParsePlanStream(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Steps) != 6 {
		t.Fatalf("steps=%d", len(p.Steps))
	}
	if p.Steps[1].Driver != "state-volume" || p.Steps[1].ID != "herdr-system-state" {
		t.Fatalf("missing Herdr volume prerequisite in rendered stream: %#v", p.Steps[1])
	}
}
