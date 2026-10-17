package statevolume

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration"
)

func TestStateVolumeDriverEnsureAndDryRun(t *testing.T) {
	calls := 0
	d := &Driver{ensureSystem: func(_ context.Context, pool, instance string) error {
		calls++
		if pool != "default" || instance != "aginctus-herdr" {
			t.Fatalf("unexpected volume destination %q %q", pool, instance)
		}
		return nil
	}}
	raw := json.RawMessage(`{"operation":"ensure","pool":"default","instance":"aginctus-herdr","class":"system"}`)
	if !d.SupportsDryRun() {
		t.Fatal("driver must support non-mutating dry run")
	}
	if err := d.Validate(raw); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("validation mutated persistent storage")
	}
	if err := d.Execute(context.Background(), raw, orchestration.ExecuteOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("dry run mutated persistent storage")
	}
	if err := d.Execute(context.Background(), raw, orchestration.ExecuteOptions{}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("ensure calls = %d, want 1", calls)
	}
}

func TestStateVolumeDriverRejectsInvalidRequestsBeforeMutation(t *testing.T) {
	calls := 0
	d := &Driver{ensureSystem: func(context.Context, string, string) error {
		calls++
		return nil
	}}
	for _, raw := range []string{
		`{}`,
		`{"operation":"delete","pool":"default","instance":"aginctus-herdr","class":"system"}`,
		`{"operation":"ensure","pool":"default","instance":"aginctus-herdr","class":"user"}`,
		`{"operation":"ensure","pool":"default","instance":"../foreign","class":"system"}`,
		`{"operation":"ensure","pool":"default","instance":"aginctus-herdr","class":"system","unexpected":true}`,
		`{"operation":"ensure","pool":"default","instance":"aginctus-herdr","class":"system"}{}`,
	} {
		if err := d.Validate(json.RawMessage(raw)); err == nil {
			t.Errorf("accepted invalid request %q", raw)
		}
		if err := d.Execute(context.Background(), json.RawMessage(raw), orchestration.ExecuteOptions{}); err == nil {
			t.Errorf("executed invalid request %q", raw)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid requests invoked ensure %d times", calls)
	}
}

func TestStateVolumeDriverPropagatesEnsureErrorAndCancellation(t *testing.T) {
	want := errors.New("foreign volume")
	calls := 0
	d := &Driver{ensureSystem: func(context.Context, string, string) error {
		calls++
		return want
	}}
	raw := json.RawMessage(`{"operation":"ensure","pool":"default","instance":"aginctus-herdr","class":"system"}`)
	err := d.Execute(context.Background(), raw, orchestration.ExecuteOptions{})
	if !errors.Is(err, want) || !strings.Contains(err.Error(), "Herdr system state") {
		t.Fatalf("ensure error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.Execute(ctx, raw, orchestration.ExecuteOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled execute = %v", err)
	}
	if calls != 1 {
		t.Fatalf("canceled operation called ensure: calls=%d", calls)
	}
}
