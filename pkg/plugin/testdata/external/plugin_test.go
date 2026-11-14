package externalplugin_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	externalplugin "example.com/aginctus-external-plugin"
	"github.com/blevinn/aginctus/pkg/orchestration"
	"github.com/blevinn/aginctus/pkg/plugin"
)

func TestExternalConsumer(t *testing.T) {
	var implementation plugin.Plugin = externalplugin.Example{}
	descriptor := implementation.Descriptor()
	if descriptor.Name != "external-example" || len(descriptor.Capabilities) != 3 {
		t.Fatalf("unexpected discovered capabilities: %+v", descriptor)
	}
	if err := plugin.Validate(context.Background(), implementation, json.RawMessage(`{"model":"fixture-model"}`)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{}`, `{"model":"unknown"}`, `{"model":"fixture-model","unexpected":true}`} {
		if err := plugin.Validate(context.Background(), implementation, json.RawMessage(raw)); err == nil {
			t.Fatalf("invalid plugin configuration accepted: %s", raw)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := plugin.Validate(ctx, implementation, json.RawMessage(`{"model":"fixture-model"}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation", err)
	}
}

func TestPublicTargetAndCheckConsumer(t *testing.T) {
	ctx := context.Background()
	raw := json.RawMessage(`{"model":"fixture-model","image":"custom:helper-v2","database_image":"custom:store-v3"}`)
	targets, err := plugin.Contribute(ctx, externalplugin.Example{}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[0].ID != "catalog" || len(targets[0].Containers) != 0 {
		t.Fatalf("expected a container-free provider target: %+v", targets)
	}
	helper := targets[1]
	if helper.ID != "helper" || !reflect.DeepEqual(helper.DependsOn, []string{"catalog"}) || len(helper.Containers) != 2 {
		t.Fatalf("expected a dependent multi-container target: %+v", helper)
	}
	if helper.Containers[0].Image.Pointer != "custom:helper-v2" || helper.Containers[1].Image.Pointer != "custom:store-v3" {
		t.Fatal("per-container image overrides were not preserved")
	}
	if helper.Containers[0].Contract.ID != "helper" || helper.Containers[0].Contract.Version != "v1" {
		t.Fatal("image override changed the plugin's guest contract")
	}
	repeated, err := plugin.Contribute(ctx, externalplugin.Example{}, raw)
	if err != nil || !reflect.DeepEqual(targets, repeated) {
		t.Fatalf("equivalent inputs produced unstable declarations: %v", err)
	}

	// A tiny offline consumer evaluates only the candidate-schema criterion. This
	// is not a runtime check executor and provides no inference/readiness evidence.
	check := targets[0].Preconditions[0]
	if check.Effect != orchestration.Offline || len(check.Access) != 0 {
		t.Fatal("candidate check unexpectedly needs external access")
	}
	outcome := orchestration.Passed
	if err := plugin.Validate(ctx, externalplugin.Example{}, raw); err != nil {
		outcome = orchestration.Failed
	}
	finding := orchestration.Finding{
		TargetID: targets[0].ID, CheckID: check.ID, Outcome: outcome,
		ObservedAt: time.Now(), Code: "candidate-schema",
	}
	if err := finding.Validate(); err != nil || finding.Outcome != orchestration.Passed {
		t.Fatalf("offline finding: %+v, %v", finding, err)
	}
	probe := targets[0].Postconditions[0]
	if probe.Effect != orchestration.ActiveProbe || !reflect.DeepEqual(probe.OptIns, []string{"live-inference"}) || !probe.Required {
		t.Fatal("inference requirement lost its effect/opt-in/blocking policy")
	}
	finding.CheckID, finding.Outcome, finding.Code = probe.ID, orchestration.Skipped, "not-authorized"
	if err := finding.Validate(); err != nil || finding.Outcome == orchestration.Passed {
		t.Fatalf("skipped probe became success: %v", err)
	}
}

type overridden struct {
	externalplugin.Example
	targets func(context.Context, json.RawMessage) ([]orchestration.Target, error)
}

func (p overridden) Targets(ctx context.Context, raw json.RawMessage) ([]orchestration.Target, error) {
	return p.targets(ctx, raw)
}

func TestContributionBoundaryRejectsInvalidOutput(t *testing.T) {
	raw := json.RawMessage(`{"model":"fixture-model"}`)
	for _, targets := range [][]orchestration.Target{
		{{ID: "same", Capability: "models"}, {ID: "same", Capability: "models"}},
		{{ID: "undeclared", Capability: "not-advertised"}},
		{{ID: "bad/id", Capability: "models"}},
		{{ID: "invalid-criteria", Capability: "models", Preconditions: []orchestration.Check{{ID: "zero"}}}},
	} {
		p := overridden{targets: func(context.Context, json.RawMessage) ([]orchestration.Target, error) { return targets, nil }}
		if result, err := plugin.Contribute(context.Background(), p, raw); err == nil || result != nil {
			t.Fatalf("invalid output accepted: %+v, %v", result, err)
		}
	}
	p := overridden{targets: func(context.Context, json.RawMessage) ([]orchestration.Target, error) {
		t.Fatal("contributed before configuration validation")
		return nil, nil
	}}
	for _, invalid := range []string{`{"model":"unknown"}`, `{"model":"fixture-model","image":""}`, `{"model":"fixture-model","unexpected":true}`} {
		if _, err := plugin.Contribute(context.Background(), p, json.RawMessage(invalid)); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	// Discovery-only implementations from A1 need not implement TargetContributor.
	var discovery plugin.Plugin = discoveryOnly{}
	if _, ok := discovery.(plugin.TargetContributor); ok {
		t.Fatal("target contribution should remain optional")
	}
}

type discoveryOnly struct{}

func (discoveryOnly) Descriptor() plugin.Descriptor                                { return externalplugin.Example{}.Descriptor() }
func (discoveryOnly) ValidateConfiguration(context.Context, json.RawMessage) error { return nil }

func TestContributionCancellationAndErrors(t *testing.T) {
	raw := json.RawMessage(`{"model":"fixture-model"}`)
	want := errors.New("contribution unavailable")
	p := overridden{targets: func(context.Context, json.RawMessage) ([]orchestration.Target, error) { return nil, want }}
	if _, err := plugin.Contribute(context.Background(), p, raw); !errors.Is(err, want) {
		t.Fatalf("error = %v, want cause %v", err, want)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.targets = func(context.Context, json.RawMessage) ([]orchestration.Target, error) {
		cancel()
		return []orchestration.Target{{ID: "valid", Capability: "models"}}, nil
	}
	if result, err := plugin.Contribute(ctx, p, raw); result != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled contribution = %+v, %v", result, err)
	}
	p.targets = func(context.Context, json.RawMessage) ([]orchestration.Target, error) {
		t.Fatal("called contribution after cancellation")
		return nil, nil
	}
	if _, err := plugin.Contribute(ctx, p, raw); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation before contribution", err)
	}
}
