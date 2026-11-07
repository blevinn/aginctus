package native

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration"
	reconciler "github.com/blevinn/aginctus/internal/orchestration/native"
)

func nativeStepConfiguration(t *testing.T, operation string, adopt bool) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(Config{
		Operation: operation,
		Resource:  "management-network",
		Name:      "aginctus-mgmt",
		Type:      "bridge",
		Config: map[string]string{
			"ipv4.address":      "auto",
			"ipv4.dhcp.gateway": "none",
			"ipv4.nat":          "false",
			"ipv4.routing":      "false",
			"ipv6.address":      "none",
		},
		Adopt: adopt,
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestNativeDriverValidatesConfigurationWithoutIncus(t *testing.T) {
	driver := NewDriver()
	if err := driver.Validate(nativeStepConfiguration(t, "upsert", false)); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if !driver.SupportsDryRun() {
		t.Fatal("SupportsDryRun() = false, want true")
	}
}

func TestNativeDriverRejectsUnsupportedConfiguration(t *testing.T) {
	driver := NewDriver()
	cases := map[string]string{
		"unknown resource":  `{"operation":"upsert","resource":"instance","name":"x","type":"bridge","config":{}}`,
		"unknown operation": `{"operation":"reset","resource":"management-network","name":"x","type":"bridge","config":{}}`,
		"missing name":      `{"operation":"upsert","resource":"management-network","type":"bridge","config":{}}`,
		"missing keys":      `{"operation":"upsert","resource":"management-network","name":"x","type":"bridge","config":{"ipv4.address":"auto"}}`,
		"unknown field":     `{"operation":"upsert","resource":"management-network","name":"x","type":"bridge","config":{},"extra":true}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if err := driver.Validate(json.RawMessage(raw)); err == nil {
				t.Fatalf("accepted invalid configuration %s", raw)
			}
		})
	}
}

func TestNativeDriverDryRunDoesNotMutate(t *testing.T) {
	var ensured, removed bool
	driver := &Driver{
		planEnsure: func(context.Context, reconciler.NetworkIntent) (reconciler.NetworkPlan, error) {
			return reconciler.NetworkPlan{}, nil
		},
		planRemove: func(context.Context, reconciler.NetworkIntent) (reconciler.NetworkRemovalPlan, error) {
			return reconciler.NetworkRemovalPlan{}, nil
		},
		ensure: func(context.Context, reconciler.NetworkIntent) (reconciler.NetworkResult, error) {
			ensured = true
			return reconciler.NetworkResult{}, nil
		},
		remove: func(context.Context, reconciler.NetworkIntent) (reconciler.NetworkResult, error) {
			removed = true
			return reconciler.NetworkResult{}, nil
		},
	}
	for _, operation := range []string{"upsert", "delete"} {
		if err := driver.Execute(context.Background(), nativeStepConfiguration(t, operation, false), orchestration.ExecuteOptions{DryRun: true}); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	}
	if ensured || removed {
		t.Fatalf("dry run mutated state: ensured=%v removed=%v", ensured, removed)
	}
}

func TestNativeDriverAppliesExplicitMutations(t *testing.T) {
	var ensured, removed bool
	driver := &Driver{
		planEnsure: reconciler.PlanLocalNetwork,
		planRemove: reconciler.PlanLocalNetworkRemoval,
		ensure: func(context.Context, reconciler.NetworkIntent) (reconciler.NetworkResult, error) {
			ensured = true
			return reconciler.NetworkResult{}, nil
		},
		remove: func(context.Context, reconciler.NetworkIntent) (reconciler.NetworkResult, error) {
			removed = true
			return reconciler.NetworkResult{}, nil
		},
	}
	if err := driver.Execute(context.Background(), nativeStepConfiguration(t, "upsert", false), orchestration.ExecuteOptions{}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if err := driver.Execute(context.Background(), nativeStepConfiguration(t, "delete", false), orchestration.ExecuteOptions{}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !ensured || !removed {
		t.Fatalf("native driver did not reconcile: ensured=%v removed=%v", ensured, removed)
	}
}

func TestNativeDriverPropagatesReconcilerFailure(t *testing.T) {
	driver := &Driver{
		planEnsure: reconciler.PlanLocalNetwork,
		planRemove: reconciler.PlanLocalNetworkRemoval,
		ensure: func(context.Context, reconciler.NetworkIntent) (reconciler.NetworkResult, error) {
			return reconciler.NetworkResult{}, &reconciler.ReconcileError{Stage: "create", Mutated: true, Uncertain: true}
		},
		remove: reconciler.RemoveLocalNetwork,
	}
	err := driver.Execute(context.Background(), nativeStepConfiguration(t, "upsert", false), orchestration.ExecuteOptions{})
	if err == nil || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("unexpected driver failure: %v", err)
	}
}
