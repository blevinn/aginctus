package workload

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/pkg/api"
)

func TestScopedStatusReadReturnsTypedAndFilteredObservations(t *testing.T) {
	loader := config.NewLoader()
	loader.SystemPath, loader.UserPath, loader.Environment = "", "", nil
	cfg, err := loader.Load(config.Options{})
	if err != nil { t.Fatal(err) }
	scope, err := cfg.IncusProjectScope()
	if err != nil { t.Fatal(err) }
	spec, err := FromConfig(cfg, "dev")
	if err != nil { t.Fatal(err) }
	observed := []InspectedInstance{{Name: spec.Name, Status: "Running", Project: scope.Name, Config: map[string]string{
		"user.aginctus.managed": "true", "user.aginctus.resource": "workload",
		"user.aginctus.role": "agent-workload", "user.aginctus.workload": "dev",
	}}}
	raw, err := json.Marshal(observed)
	if err != nil { t.Fatal(err) }
	reader := ScopedStatusReader{
		Config: cfg, MaxBytes: 8192,
		Verify: func(context.Context, incusscope.ProjectScope) error { return nil },
		Inspect: func(context.Context, incusscope.ProjectScope) ([]byte, error) { return raw, nil },
	}
	for _, running := range []bool{false, true} {
		result, err := reader.ReadScopedWorkloadStatus(context.Background(), api.ScopedWorkloadStatusRequest{Project: scope.Name, RunningOnly: running})
		if err != nil { t.Fatal(err) }
		if result.Kind != "ScopedWorkloadStatusList" || result.Project != scope.Name || len(result.Items) != 1 ||
			result.Items[0].Name != "dev" || result.Items[0].State != "Running" {
			t.Fatalf("unexpected scoped status: %#v", result)
		}
	}
	observed[0].Status = "Stopped"
	raw, _ = json.Marshal(observed)
	result, err := reader.ReadScopedWorkloadStatus(context.Background(), api.ScopedWorkloadStatusRequest{Project: scope.Name, RunningOnly: true})
	if err != nil || len(result.Items) != 0 || result.Items == nil {
		t.Fatalf("running-only filter=%#v err=%v", result, err)
	}
	result, err = reader.ReadScopedWorkloadStatus(context.Background(), api.ScopedWorkloadStatusRequest{Project: scope.Name, Name:"dev"})
	if err != nil || len(result.Items) != 1 || result.Items[0].State != "Stopped" {
		t.Fatalf("stopped status=%#v err=%v", result, err)
	}
}

func TestScopedStatusRejectsMismatchAndForeignOwnershipWithoutPartialResults(t *testing.T) {
	loader := config.NewLoader()
	loader.SystemPath, loader.UserPath, loader.Environment = "", "", nil
	cfg, err := loader.Load(config.Options{})
	if err != nil { t.Fatal(err) }
	scope, _ := cfg.IncusProjectScope()
	inspected := 0
	reader := ScopedStatusReader{
		Config: cfg, MaxBytes: 8192,
		Verify: func(context.Context, incusscope.ProjectScope) error { return nil },
		Inspect: func(context.Context, incusscope.ProjectScope) ([]byte, error) {
			inspected++
			return []byte(`[{"name":"aginctus-dev","status":"Running","config":{"user.aginctus.managed":"true","user.aginctus.resource":"foreign"},"project":"`+scope.Name+`"}]`), nil
		},
	}
	if _, err := reader.ReadScopedWorkloadStatus(context.Background(), api.ScopedWorkloadStatusRequest{Project:"other"}); err == nil || inspected != 0 {
		t.Fatalf("read wrong project, inspected=%d err=%v", inspected, err)
	}
	result, err := reader.ReadScopedWorkloadStatus(context.Background(), api.ScopedWorkloadStatusRequest{Project:scope.Name})
	if err == nil || len(result.Items) != 0 {
		t.Fatalf("foreign ownership returned data: %#v err=%v", result, err)
	}
	_, err = reader.ReadScopedWorkloadStatus(context.Background(), api.ScopedWorkloadStatusRequest{Project:scope.Name, Name:"missing"})
	if err == nil || !strings.Contains(err.Error(), "configuration") {
		t.Fatalf("unknown configured workload: %v", err)
	}
	ctx,cancel := context.WithCancel(context.Background())
	cancel()
	_,err = reader.ReadScopedWorkloadStatus(ctx, api.ScopedWorkloadStatusRequest{Project:scope.Name})
	if !errors.Is(err,context.Canceled) { t.Fatal(err) }
}
