package sshaccess

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration"
)

type fakeRuntime struct {
	inspectCalls   int
	reconcileCalls int
	revokeCalls    int
	err            error
}

func (f *fakeRuntime) Inspect(context.Context, Config) (State, error) {
	f.inspectCalls++
	return State{}, f.err
}

func (f *fakeRuntime) Reconcile(context.Context, Config) error {
	f.reconcileCalls++
	return f.err
}

func (f *fakeRuntime) Revoke(context.Context, Config) error {
	f.revokeCalls++
	return f.err
}

func validConfig(t *testing.T, operation Operation) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(Config{
		Operation:      operation,
		ClientInstance: "aginctus-herdr",
		WorkloadID:     "dev",
		TargetInstance: "aginctus-dev",
		TargetAccount:  "agent",
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestDryRunInspectsWithoutMutation(t *testing.T) {
	runtime := &fakeRuntime{}
	driver := New(runtime)

	if err := driver.Execute(context.Background(), validConfig(t, Reconcile), orchestration.ExecuteOptions{DryRun: true}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if runtime.inspectCalls != 1 || runtime.reconcileCalls != 0 || runtime.revokeCalls != 0 {
		t.Fatalf("runtime calls = inspect:%d reconcile:%d revoke:%d", runtime.inspectCalls, runtime.reconcileCalls, runtime.revokeCalls)
	}
}

func TestExecuteMapsOperations(t *testing.T) {
	for _, operation := range []Operation{Reconcile, Revoke} {
		t.Run(string(operation), func(t *testing.T) {
			runtime := &fakeRuntime{}
			driver := New(runtime)
			if err := driver.Execute(context.Background(), validConfig(t, operation), orchestration.ExecuteOptions{}); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if operation == Reconcile && runtime.reconcileCalls != 1 {
				t.Fatalf("reconcile calls = %d", runtime.reconcileCalls)
			}
			if operation == Revoke && runtime.revokeCalls != 1 {
				t.Fatalf("revoke calls = %d", runtime.revokeCalls)
			}
		})
	}
}

func TestValidateRejectsRootTarget(t *testing.T) {
	raw := validConfig(t, Reconcile)
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg.TargetAccount = "root"
	raw, _ = json.Marshal(cfg)

	err := New(&fakeRuntime{}).Validate(raw)
	if err == nil || !strings.Contains(err.Error(), "must not be root") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsUnknownFields(t *testing.T) {
	raw := json.RawMessage(`{"operation":"reconcile","clientInstance":"aginctus-herdr","workloadID":"dev","targetInstance":"aginctus-dev","targetAccount":"agent","secret":"nope"}`)
	if err := New(&fakeRuntime{}).Validate(raw); err == nil {
		t.Fatal("Validate() error = nil, want unknown-field error")
	}
}

func TestExecuteReportsRuntimeFailure(t *testing.T) {
	driver := New(&fakeRuntime{err: errors.New("guest unavailable")})
	err := driver.Execute(context.Background(), validConfig(t, Reconcile), orchestration.ExecuteOptions{})
	if err == nil || !strings.Contains(err.Error(), "guest unavailable") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestExecuteRequiresRuntime(t *testing.T) {
	err := New(nil).Execute(context.Background(), validConfig(t, Reconcile), orchestration.ExecuteOptions{})
	if err == nil || !strings.Contains(err.Error(), "runtime is not configured") {
		t.Fatalf("Execute() error = %v", err)
	}
}


func TestValidateRejectsUnsafeWorkloadID(t *testing.T) {
	raw := validConfig(t, Reconcile)
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg.WorkloadID = "../dev"
	raw, _ = json.Marshal(cfg)

	err := New(&fakeRuntime{}).Validate(raw)
	if err == nil || !strings.Contains(err.Error(), "safe identifier") {
		t.Fatalf("Validate() error = %v", err)
	}
}
