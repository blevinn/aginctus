package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func fakeOperatorUnitProperties(load, active, file string) []byte {
	return []byte("LoadState=" + load + "\nActiveState=" + active + "\nUnitFileState=" + file + "\n")
}

func stubOperatorUnitProbe(t *testing.T, probe func(string, string) ([]byte, error)) {
	t.Helper()
	before := inspectOperatorUnit
	inspectOperatorUnit = func(_ context.Context, manager, unit string) ([]byte, error) { return probe(manager, unit) }
	t.Cleanup(func() { inspectOperatorUnit = before })
}

func TestOperatorUnitsInventoryDoesNotInferControllerOwnership(t *testing.T) {
	missing := fakeOperatorUnitProperties("not-found", "inactive", "")
	active := fakeOperatorUnitProperties("loaded", "active", "enabled")
	installed := fakeOperatorUnitProperties("loaded", "inactive", "disabled")
	for _, tc := range []struct {
		name string
		system, user []byte
		denyUser bool
		wantCode int
		want string
	}{
		{"both absent", missing, missing, false, 0, "system=absent user=absent"},
		{"system active", active, missing, false, 0, "system=active user=absent"},
		{"user active", missing, active, false, 0, "system=absent user=active"},
		{"both installed", installed, installed, false, 0, "system=installed user=installed"},
		{"both active", active, active, false, 0, "system=active user=active"},
		{"user manager unavailable", missing, missing, true, 1, "system=absent user=unverified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			stubOperatorUnitProbe(t, func(manager, unit string) ([]byte, error) {
				calls++
				if !strings.HasSuffix(unit, ".service") { t.Errorf("unexpected unit %s", unit) }
				if manager == "system" { return tc.system, nil }
				if manager != "user" { t.Errorf("unexpected manager %s", manager); return nil, nil }
				if tc.denyUser { return nil, errors.New("private-unit-env-value-must-never-be-printed") }
				return tc.user, nil
			})
			var stdout, stderr bytes.Buffer
			code := renderOperatorUnitsAudit(context.Background(), &stdout, &stderr)
			if code != tc.wantCode || !strings.Contains(stdout.String(), tc.want) {
				t.Fatalf("code=%d want=%d out=%q err=%q", code, tc.wantCode, stdout.String(), stderr.String())
			}
			if calls != 2*len(operatorLifecycleUnits) { t.Fatalf("calls=%d", calls) }
			if strings.Contains(stdout.String()+stderr.String(), "private-unit-env-value") { t.Fatal("unit error leaked") }
			if strings.Contains(stdout.String(), "Selected lifecycle authority") || strings.Contains(stdout.String(), "No competing known lifecycle units") {
				t.Fatal("inventory made unsupported controller authority claim")
			}
		})
	}
}

func TestOperatorUnitsInventoryRejectsOwnerSelection(t *testing.T) {
	for _, args := range [][]string{{"--owner=user"}, {"--owner", "system"}, {"--force"}} {
		var stdout, stderr bytes.Buffer
		if got := runOperatorUnitsAudit(context.Background(), args, &stdout, &stderr); got != 2 {
			t.Errorf("args=%q returned code=%d, want 2", args, got)
		}
	}
}
