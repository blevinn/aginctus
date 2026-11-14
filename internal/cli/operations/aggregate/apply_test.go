package aggregateops

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workloadops "github.com/blevinn/aginctus/internal/cli/operations/workload"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/orchestration"
	composedriver "github.com/blevinn/aginctus/internal/orchestration/drivers/compose"
)

func TestExecutePlanRejectsEveryForeignComposeProjectBeforeSecrets(t *testing.T) {
	for _, foreignFirst := range []bool{true, false} {
		name := "foreign after configured gateway"
		if foreignFirst {
			name = "foreign before configured gateway"
		}
		t.Run(name, func(t *testing.T) {
			state := t.TempDir()
			t.Setenv("XDG_STATE_HOME", state)
			// Keep the authority regression hermetic even if it accidentally
			// proceeds past authorization and reaches the Compose executor.
			t.Setenv("INCUS_SOCKET", filepath.Join(t.TempDir(), "unavailable.socket"))
			loader := config.NewLoader()
			loader.SystemPath = ""
			loader.UserPath = ""
			loader.Environment = nil
			cfg, err := loader.Load(config.Options{})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := Plan(cfg, workloadops.IDs)
			if err != nil {
				t.Fatal(err)
			}
			var gateway orchestration.Step
			for _, step := range plan.Steps {
				if step.Driver == "compose" {
					gateway = step
					break
				}
			}
			if gateway.Driver != "compose" {
				t.Fatal("expected a configured gateway Compose step")
			}
			var foreignConfig composedriver.Config
			if err := json.Unmarshal(gateway.Configuration, &foreignConfig); err != nil {
				t.Fatal(err)
			}
			foreignConfig.Project = "foreign-project"
			raw, err := json.Marshal(foreignConfig)
			if err != nil {
				t.Fatal(err)
			}
			foreign := orchestration.Step{ID: "foreign", Driver: "compose", Configuration: raw}
			plan.Steps = []orchestration.Step{gateway, foreign}
			if foreignFirst {
				plan.Steps = []orchestration.Step{foreign, gateway}
			}
			err = ExecutePlan(t.Context(), plan, cfg)
			if err == nil || !strings.Contains(err.Error(), `compose project "foreign-project" is not configured gateway project`) {
				t.Errorf("expected foreign-project rejection, got %v", err)
			}
			entries, err := os.ReadDir(state)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatal("rejected plan initialized gateway credential state")
			}
		})
	}
}

func TestExecutePlanCancellationDoesNotInitializeSecrets(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("INCUS_SOCKET", filepath.Join(t.TempDir(), "unavailable.socket"))
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	cfg, err := loader.Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Plan(cfg, workloadops.IDs)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := ExecutePlan(ctx, plan, cfg); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v, want cancellation", err)
	}
	entries, err := os.ReadDir(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("canceled plan initialized gateway credential state")
	}
}
