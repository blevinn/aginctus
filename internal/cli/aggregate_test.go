package cli

import (
	"bytes"
	"context"
	"encoding/json"
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
	expected := []string{"management-network", "herdr-client", "gateway", "workload-dev"}
	if len(plan.Steps) != len(expected) {
		t.Fatalf("steps = %d, want %d: %s", len(plan.Steps), len(expected), first)
	}
	for i, want := range expected {
		if plan.Steps[i].ID != want {
			t.Fatalf("step %d = %q, want %q", i, plan.Steps[i].ID, want)
		}
	}
}

func TestAggregateApplyRequiresRender(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"apply"}, &out, &errOut, &fakeIncusClient{}, &fakeConfigLoader{})
	if code != 2 || !strings.Contains(errOut.String(), "not implemented") {
		t.Fatalf("code = %d, stderr = %q", code, errOut.String())
	}
}
