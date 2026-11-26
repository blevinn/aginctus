package worker

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/blevinn/aginctus/internal/testing/testenv"
)

func TestScenarioPlanOffline(t *testing.T) {
	var out bytes.Buffer
	cmd := New(Handlers{})
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"scenario", "plan", "native-instance"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var p testenv.ManagedScenarioPlan
	if err := json.Unmarshal(out.Bytes(), &p); err != nil || p.Status != "not_admitted" || p.Network != "offline" {
		t.Fatalf("unexpected plan: %+v %v", p, err)
	}
}

func TestScenarioPlanRejectsUnsupported(t *testing.T) {
	for _, args := range [][]string{
		{"scenario", "plan", "full-stack"},
		{"scenario", "plan", "native-oci"},
		{"scenario", "plan", "native-instance", "extra"},
		{"scenario", "run", "native-instance"},
	} {
		cmd := New(Handlers{})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}
