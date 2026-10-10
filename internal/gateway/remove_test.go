package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestValidateGatewayCleanup(t *testing.T) {
	project := "aginctus-gateway"
	owned := cleanupProject{Name: project, Config: map[string]string{
		"user.aginctus.managed":    "true",
		"user.aginctus.resource":   "compose-project",
		"user.aginctus.deployment": project,
	}}
	instances := []cleanupInstance{{Name: project + "-postgres-1", Config: map[string]string{
		"user.aginctus.managed":    "true",
		"user.aginctus.resource":   "compose-instance",
		"user.aginctus.deployment": project,
	}}}
	if err := validateGatewayCleanup(project, owned, instances, false); err != nil {
		t.Fatal(err)
	}
	legacy := cleanupProject{Name: project}
	if err := validateGatewayCleanup(project, legacy, instances, false); err == nil {
		t.Fatal("unowned project accepted without force")
	}
	if err := validateGatewayCleanup(project, legacy, instances, true); err != nil {
		t.Fatal(err)
	}
	if err := validateGatewayCleanup(project, legacy, []cleanupInstance{{Name: "unrelated-instance"}}, true); err == nil {
		t.Fatal("unexpected instance accepted with force")
	}
	if err := validateGatewayCleanup(project, owned, []cleanupInstance{{Name: project + "-litellm-1"}}, false); err == nil {
		t.Fatal("unowned instance accepted without force")
	}
}

func TestRemoveGatewayForceRepairsOnlyConfiguredProject(t *testing.T) {
	previous := runIncus
	defer func() { runIncus = previous }()
	var actions []string
	runIncus = func(_ context.Context, args ...string) ([]byte, error) {
		actions = append(actions, strings.Join(args, " "))
		switch {
		case len(args) >= 2 && args[0] == "project" && args[1] == "list":
			return json.Marshal([]cleanupProject{{Name: "aginctus-gateway"}})
		case len(args) > 0 && args[0] == "list":
			return json.Marshal([]cleanupInstance{{Name: "aginctus-gateway-postgres-1"}})
		default:
			return nil, nil
		}
	}
	spec := Spec{ID: "test", Project: "aginctus-gateway", Network: "test", LiteLLMImage: "litellm", PostgresImage: "postgres"}
	if err := spec.Remove(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(actions, "\n")
	for _, token := range []string{
		"delete aginctus-gateway-postgres-1 --project aginctus-gateway --force",
		"project set aginctus-gateway user.aginctus.deployment=aginctus-gateway",
	} {
		if !strings.Contains(joined, token) {
			t.Fatalf("missing %q in commands:\n%s", token, joined)
		}
	}
	for _, action := range actions {
		if strings.Contains(action, "volume") || strings.Contains(action, "project delete") {
			t.Fatalf("destructive data operation: %s", action)
		}
	}
}

func TestRemoveGatewayFailurePreventsWrites(t *testing.T) {
	previous := runIncus
	defer func() { runIncus = previous }()
	var writes int
	runIncus = func(_ context.Context, args ...string) ([]byte, error) {
		switch args[0] {
		case "project":
			if args[1] == "list" {
				return json.Marshal([]cleanupProject{{Name: "aginctus-gateway"}})
			}
		case "list":
			return json.Marshal([]cleanupInstance{{Name: "somebody-elses-instance"}})
		}
		writes++
		return nil, fmt.Errorf("unexpected mutation")
	}
	spec := Spec{ID: "test", Project: "aginctus-gateway", Network: "test", LiteLLMImage: "litellm", PostgresImage: "postgres"}
	if err := spec.Remove(context.Background(), true); err == nil || writes != 0 {
		t.Fatalf("failure = %v, mutations = %d", err, writes)
	}
}

func TestGatewayServiceNameIncusComposeNaming(t *testing.T) {
	project := "aginctus-gateway"
	for _, name := range []string{"litellm-1", "postgres-1", "litellm-2", "postgres-12", "aginctus-gateway-litellm-1", "aginctus-gateway-postgres-1"} {
		if !gatewayServiceName(project, name) {
			t.Errorf("valid gateway service %q rejected", name)
		}
	}
	for _, name := range []string{"unrelated-1", "litellm-backup-1", "postgres-data", "litellm-1-backup", "litellm-0", "postgres-xyz", "some-other-project-postgres-1"} {
		if gatewayServiceName(project, name) {
			t.Errorf("unexpected instance %q accepted", name)
		}
	}
}

func TestRemoveGatewayAlreadyAbsentIsNoop(t *testing.T) {
	previous := runIncus
	t.Cleanup(func() { runIncus = previous })
	calls := 0
	runIncus = func(_ context.Context, args ...string) ([]byte, error) {
		calls++
		if strings.Join(args, " ") != "project list --format=json" {
			t.Fatalf("unexpected command: %v", args)
		}
		return []byte(`[{"name":"default","config":{}}]`), nil
	}
	spec := Spec{ID: "test", Project: "aginctus-gateway", Network: "test", LiteLLMImage: "litellm", PostgresImage: "postgres"}
	for i := 0; i < 2; i++ {
		if err := spec.Remove(context.Background(), false); err != nil {
			t.Fatalf("remove attempt %d: %v", i, err)
		}
	}
	if calls != 2 {
		t.Fatalf("expected two read-only project listings, got %d", calls)
	}
}

func TestRemoveGatewayDoesNotMaskInspectionErrors(t *testing.T) {
	previous := runIncus
	t.Cleanup(func() { runIncus = previous })
	spec := Spec{ID: "test", Project: "aginctus-gateway", Network: "test", LiteLLMImage: "litellm", PostgresImage: "postgres"}
	for _, tc := range []struct {
		name string
		data []byte
		err  error
	}{
		{"daemon failure", nil, fmt.Errorf("daemon unreachable")},
		{"invalid JSON", []byte(`{`), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runIncus = func(context.Context, ...string) ([]byte, error) { return tc.data, tc.err }
			if err := spec.Remove(context.Background(), false); err == nil {
				t.Fatal("inspection error was swallowed")
			}
		})
	}
}

func TestRemoveGatewayEmptyOwnedProjectIsNoop(t *testing.T) {
	previous := runIncus
	t.Cleanup(func() { runIncus = previous })
	project := "aginctus-gateway"
	commands := []string{}
	runIncus = func(_ context.Context, args ...string) ([]byte, error) {
		commands = append(commands, strings.Join(args, " "))
		if args[0] == "project" {
			return json.Marshal([]cleanupProject{{Name: project, Config: map[string]string{
				"user.aginctus.managed": "true", "user.aginctus.resource": "compose-project", "user.aginctus.deployment": project,
			}}})
		}
		if args[0] == "list" {
			return []byte("[]"), nil
		}
		t.Fatalf("unexpected mutation: %v", args)
		return nil, nil
	}
	spec := Spec{ID: "test", Project: project, Network: "test", LiteLLMImage: "litellm", PostgresImage: "postgres"}
	if err := spec.Remove(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 {
		t.Fatalf("unexpected commands: %v", commands)
	}
}
