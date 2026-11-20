package workload

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

func TestScopedReadRejectsUnverifiedAndMalformedInventory(t *testing.T) {
	loader := config.NewLoader()
	loader.SystemPath, loader.UserPath, loader.Environment = "", "", nil
	cfg, err := loader.Load(config.Options{})
	if err != nil { t.Fatal(err) }
	scope, err := cfg.IncusProjectScope()
	if err != nil { t.Fatal(err) }
	verified := 0
	read := 0
	verify := func(context.Context, incusscope.ProjectScope) error { verified++; return nil }
	inspect := func(context.Context, incusscope.ProjectScope) ([]byte, error) {
		read++
		return []byte(`[{"name":"a","status":"Running","project":"` + scope.Name + `"}]`), nil
	}
	if _, _, err := ReadScopedManagedInstances(context.Background(), cfg, "wrong", verify, inspect, 1024); err == nil || verified != 0 || read != 0 {
		t.Fatalf("wrong project unexpectedly inspected: %v", err)
	}
	verify = func(context.Context, incusscope.ProjectScope) error { verified++; return errors.New("secret evidence") }
	if _, _, err := ReadScopedManagedInstances(context.Background(), cfg, scope.Name, verify, inspect, 1024); err == nil || strings.Contains(err.Error(), "secret") || read != 0 {
		t.Fatalf("unverified project disclosed or inspected: %v", err)
	}
	verify = func(context.Context, incusscope.ProjectScope) error { return nil }
	for _, bad := range []string{
		"null",
		`[{"name":"a"},{"name":"a"}]`,
		`[{"name":"a","project":"other"}]`,
		`[{"name":""}]`,
	} {
		inspect = func(context.Context, incusscope.ProjectScope) ([]byte, error) { return []byte(bad), nil }
		if _, _, err := ReadScopedManagedInstances(context.Background(), cfg, scope.Name, verify, inspect, 1024); err == nil {
			t.Fatalf("accepted invalid inventory %s", bad)
		}
	}
}

func TestScopedFindOwnershipCollision(t *testing.T) {
	instances := []InspectedInstance{{Name: "a", Status: "Running", Config: map[string]string{
		"user.aginctus.managed":"true", "user.aginctus.resource":"workload",
		"user.aginctus.role":"agent-workload", "user.aginctus.workload":"dev",
	}}}
	if status, err := FindScopedManagedInstance(instances, "a", "workload", "agent-workload", "dev"); err != nil || status != "Running" {
		t.Fatalf("owned status=%q err=%v", status, err)
	}
	if _, err := FindScopedManagedInstance(instances, "a", "workload", "agent-workload", "other"); err == nil {
		t.Fatal("foreign workload accepted")
	}
	if state, err := FindScopedManagedInstance(instances, "absent", "workload", "agent-workload", "dev"); err != nil || state != "" {
		t.Fatalf("absent status=%q err=%v", state, err)
	}
}
