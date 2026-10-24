package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

func TestOperatorProjectPlanArgs(t *testing.T) {
	for _, args := range [][]string{
		nil, {}, {"--project"}, {"--project="}, {"--project=default"},
		{"--project", "default"}, {"--project", "bad space"},
		{"--project", "aginctus", "--force"}, {"--project", "../escape"},
	} {
		if _, err := parseOperatorProjectPlanArgs(args); err == nil {
			t.Errorf("unsafe project arguments accepted: %q", args)
		}
	}
	for _, args := range [][]string{
		{"--project=aginctus"}, {"--project", "aginctus"},
		{"--project", "aginctus-dev"},
	} {
		if _, err := parseOperatorProjectPlanArgs(args); err != nil {
			t.Errorf("valid project arguments rejected: %q: %v", args, err)
		}
	}
}

func TestOperatorProjectPlanConservativeReadOnly(t *testing.T) {
	scope, err := incusscope.NewProjectScope("aginctus")
	if err != nil {
		t.Fatal(err)
	}
	toJSON := func(projects []observedProject) []byte {
		t.Helper()
		data, err := json.Marshal(projects)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	foreign := scope.DesiredConfig()
	foreign["user.aginctus.deployment"] = "foreign"
	drift := scope.DesiredConfig()
	drift["features.networks"] = "true"
	tests := []struct {
		name string
		data []byte
		err error
		code int
		status string
	}{
		{"owned compatible", toJSON([]observedProject{{Name: "aginctus", Config: scope.DesiredConfig()}}), nil, 0, "compatible"},
		{"foreign owned name", toJSON([]observedProject{{Name: "aginctus", Config: foreign}}), nil, 1, "foreign"},
		{"network policy drift", toJSON([]observedProject{{Name: "aginctus", Config: drift}}), nil, 1, "policy-drift"},
		{"restricted list cannot prove absent", []byte("[]"), nil, 1, "not-visible"},
		{"other project does not authorize target", toJSON([]observedProject{{Name: "default", Config: scope.DesiredConfig()}}), nil, 1, "not-visible"},
		{"denied", nil, errors.New("SECRET=must-not-appear"), 1, ""},
		{"malformed", []byte("{bad"), nil, 1, ""},
		{"null", []byte("null"), nil, 1, ""},
		{"oversized", bytes.Repeat([]byte("x"), maxProjectListBytes+1), nil, 1, ""},
		{"duplicate project", toJSON([]observedProject{{Name: "aginctus", Config: scope.DesiredConfig()}, {Name: "aginctus", Config: scope.DesiredConfig()}}), nil, 1, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := inspectOperatorIncus
			t.Cleanup(func() { inspectOperatorIncus = before })
			calls := 0
			inspectOperatorIncus = func(_ context.Context, args ...string) ([]byte, error) {
				calls++
				if strings.Join(args, " ") != "project list --format json" {
					t.Fatalf("unexpected incus command: %q", args)
				}
				return tc.data, tc.err
			}
			var out, errOut bytes.Buffer
			code := runOperatorProjectPlan(context.Background(), []string{"--project", "aginctus"}, &out, &errOut)
			if code != tc.code || calls != 1 {
				t.Fatalf("code=%d want=%d calls=%d stderr=%q", code, tc.code, calls, errOut.String())
			}
			all := out.String() + errOut.String()
			if strings.Contains(all, "SECRET=must-not-appear") || strings.Contains(all, "incus project switch") {
				t.Fatal("sensitive output or project switch exposed")
			}
			if tc.status != "" {
				var report projectPlanReport
				if err := json.Unmarshal(out.Bytes(), &report); err != nil {
					t.Fatalf("invalid plan JSON %q: %v", out.String(), err)
				}
				if report.Project != "aginctus" || report.Status != tc.status ||
					report.DesiredConfig["features.networks"] != "false" ||
					report.DesiredConfig["user.aginctus.resource"] != "deployment-project" {
					t.Errorf("unexpected project plan %+v", report)
				}
			} else if out.Len() != 0 {
				t.Errorf("unexpected success report on failed probe: %q", out.String())
			}
		})
	}
}

func TestOperatorProjectPlanRejectsInvalidNameBeforeIncus(t *testing.T) {
	before := inspectOperatorIncus
	t.Cleanup(func() { inspectOperatorIncus = before })
	inspectOperatorIncus = func(_ context.Context, args ...string) ([]byte, error) {
		t.Fatalf("Incus called for invalid explicit project: %q", args)
		return nil, nil
	}
	for _, args := range [][]string{{"--project=default"}, {"--project", "../other"}, nil} {
		var out, errOut bytes.Buffer
		if code := runOperatorProjectPlan(context.Background(), args, &out, &errOut); code != 2 || out.Len() != 0 {
			t.Errorf("args=%q code=%d out=%q err=%q", args, code, out.String(), errOut.String())
		}
	}
}
