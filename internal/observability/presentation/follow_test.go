package presentation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	collection "github.com/blevinn/aginctus/internal/observability"
	"github.com/blevinn/aginctus/internal/observability/presentation"
	"github.com/blevinn/aginctus/internal/orchestration"
	contract "github.com/blevinn/aginctus/pkg/observability"
)

type driver struct{ failure error }

func (driver) ValidateContext(context.Context, json.RawMessage) error { return nil }
func (driver) SupportsDryRun() bool                                   { return false }
func (d driver) Execute(context.Context, json.RawMessage, orchestration.ExecuteOptions) error {
	return d.failure
}

func execute(t *testing.T, monitor *collection.Monitor, count int, failure error) orchestration.Result {
	t.Helper()
	plan := orchestration.Plan{APIVersion: orchestration.APIVersion, Kind: orchestration.Kind, Metadata: orchestration.Metadata{Name: "SECRET_PLAN"}}
	for i := range count {
		plan.Steps = append(plan.Steps, orchestration.Step{ID: fmt.Sprintf("SECRET_STEP_%d", i), Driver: "fixture", Configuration: json.RawMessage(`{"token":"SECRET_CONFIGURATION"}`)})
	}
	result, err := orchestration.NewEngine(map[string]orchestration.Driver{"fixture": driver{failure}}).Execute(t.Context(), plan, orchestration.ExecuteOptions{Monitor: monitor})
	if (err != nil) != (failure != nil) {
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
			result := execute(t, monitor, 300, nil)
			if !result.Report.Status.Finished || len(result.Operations) != 300 {
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
				if !strings.Contains(writer.String(), "action=resnapshot") || !strings.Contains(writer.String(), "finished=true") {
					t.Fatal(writer.String())
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
				if !gap || !final.Finished || final.Run.State != contract.Completed || final.Watermark != result.Report.Status.Watermark {
					t.Fatal("gap/final status not rendered")
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
