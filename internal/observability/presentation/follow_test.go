package presentation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	collection "github.com/blevinn/aginctus/internal/observability"
	"github.com/blevinn/aginctus/internal/observability/presentation"
	"github.com/blevinn/aginctus/internal/orchestration"
	contract "github.com/blevinn/aginctus/pkg/observability"
	checks "github.com/blevinn/aginctus/pkg/orchestration"
)

type driver struct{ failure error }

func (driver) ValidateContext(context.Context, json.RawMessage) error { return nil }
func (driver) SupportsDryRun() bool                                   { return false }
func (d driver) Execute(context.Context, json.RawMessage, orchestration.ExecuteOptions) error {
	return d.failure
}

func execute(t *testing.T, monitor *collection.Monitor, count int, failure error, postconditions ...orchestration.BoundCheck) orchestration.Result {
	t.Helper()
	plan := orchestration.Plan{APIVersion: orchestration.APIVersion, Kind: orchestration.Kind, Metadata: orchestration.Metadata{Name: "SECRET_PLAN"}}
	for i := range count {
		plan.Steps = append(plan.Steps, orchestration.Step{ID: fmt.Sprintf("SECRET_STEP_%d", i), Driver: "fixture", Configuration: json.RawMessage(`{"token":"SECRET_CONFIGURATION"}`)})
	}
	engine := orchestration.NewEngine(map[string]orchestration.Driver{"fixture": driver{failure}})
	wantErr := failure
	if len(postconditions) > 0 {
		engine = engine.WithConditions(map[string]orchestration.StepConditions{plan.Steps[count-1].ID: {
			Scope: orchestration.TargetScope{Deployment: "lab", Instance: "one", Target: "target"}, Postconditions: postconditions,
		}}, orchestration.CheckPolicy{})
		wantErr = orchestration.ErrConditionNotMet
	}
	result, err := engine.Execute(t.Context(), plan, orchestration.ExecuteOptions{Monitor: monitor})
	if !errors.Is(err, wantErr) {
		t.Fatalf("unexpected execution result: %v", err)
	}
	return result
}

type blockedWriter struct {
	bytes.Buffer
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *blockedWriter) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.entered); <-w.release })
	return w.Buffer.Write(data)
}

func TestSlowPresentationCannotBlockEngineAndAutomaticallyResnapshotsGap(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			monitor := collection.NewMonitor()
			writer := &blockedWriter{entered: make(chan struct{}), release: make(chan struct{})}
			release := sync.OnceFunc(func() { close(writer.release) })
			defer release()
			sink := presentation.Text(writer)
			if format == "json" {
				sink = presentation.JSONLines(writer)
			}
			done := make(chan error, 1)
			go func() { done <- presentation.Follow(t.Context(), monitor, sink) }()
			<-writer.entered // observer registered and blocked independently of execution
			check := orchestration.BoundCheck{
				Check: checks.Check{ID: "criterion", Scope: "fixture target", Claim: "fixture criterion", Required: true, Effect: checks.Offline, Timeout: time.Second},
				Evaluate: func(context.Context) (checks.Finding, error) {
					return checks.Finding{TargetID: "target", CheckID: "criterion", Outcome: checks.Skipped, ObservedAt: time.Now(), Code: "fixture"}, nil
				},
			}
			result := execute(t, monitor, 300, nil, check)
			if !result.Report.Status.Finished || result.Report.Dropped == 0 || len(result.Operations) != 300 || len(result.Steps) != 299 {
				t.Fatal("sink changed execution")
			}
			release()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if strings.Contains(writer.String(), "SECRET_") {
				t.Fatal("configuration/plan name leaked")
			}
			if format == "text" {
				if !strings.Contains(writer.String(), "action=resnapshot") || !strings.Contains(writer.String(), "finished=true") ||
					!strings.Contains(writer.String(), `kind="operation" execution_state="completed" reason="" step="step-300"`) ||
					!strings.Contains(writer.String(), `kind="postcondition-verification" execution_state="skipped" reason="condition-not-met" step="step-300"`) {
					t.Fatal("text resnapshot lost driver completion or skipped verification")
				}
			} else {
				decoder := json.NewDecoder(&writer.Buffer)
				var final contract.Snapshot
				gap := false
				for {
					var record struct {
						Snapshot *contract.Snapshot
						Update   *contract.Update
					}
					if err := decoder.Decode(&record); errors.Is(err, io.EOF) {
						break
					} else if err != nil {
						t.Fatal(err)
					}
					if record.Update != nil {
						gap = gap || record.Update.Gap
					}
					if record.Snapshot != nil {
						final = *record.Snapshot
					}
				}
				if !gap || !final.Finished || final.Run.State != contract.Blocked || final.Watermark != result.Report.Status.Watermark || final.Validate() != nil {
					t.Fatal("gap/final status not rendered")
				}
				for kind, want := range map[contract.Kind]contract.State{contract.Operation: contract.Completed, contract.PostconditionVerification: contract.Skipped} {
					if !slices.ContainsFunc(final.Entries, func(e contract.Event) bool {
						return e.OperationID == "step-300" && e.Kind == kind && e.State == want
					}) {
						t.Fatalf("JSON resnapshot lost %s=%s for step-300", kind, want)
					}
				}
			}
		})
	}
}

func TestConsumerFailureAndCancellationAreIsolatedFromExecution(t *testing.T) {
	monitor := collection.NewMonitor()
	failure := errors.New("optional sink unavailable")
	if err := presentation.Follow(t.Context(), monitor, presentation.JSONLines(failingWriter{failure})); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	result := execute(t, monitor, 1, nil)
	if result.Report.Status.Run.State != contract.Completed {
		t.Fatal("observer failure changed execution")
	}
	for range collection.SubscriberLimit + 1 {
		s, err := monitor.Subscribe(monitor.Snapshot().Cursor())
		if err != nil {
			t.Fatal("failed consumer leaked a subscription", err)
		}
		s.Close()
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := presentation.Follow(ctx, collection.NewMonitor(), presentation.Text(io.Discard)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestFinalPresentationPreservesUncertainPartialExecutionWithoutRawError(t *testing.T) {
	monitor := collection.NewMonitor()
	result := execute(t, monitor, 1, errors.New("SECRET_DRIVER_ERROR"))
	var output bytes.Buffer
	if err := presentation.Follow(t.Context(), monitor, presentation.Text(&output)); err != nil {
		t.Fatal(err)
	}
	if result.Report.Status.Run.State != contract.Uncertain || !strings.Contains(output.String(), `execution_state="uncertain"`) || strings.Contains(output.String(), "SECRET_") {
		t.Fatal("presentation lost uncertainty or leaked raw input", output.String())
	}
}
