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

func TestOperatorProjectEnsureArgs(t *testing.T) {
	for _, args := range [][]string{
		nil, {}, {"--project", "aginctus"}, {"--project=aginctus"},
		{"--project", "default", "--confirm-create"},
		{"--project=../outside", "--confirm-create"},
		{"--confirm-create", "--project=aginctus"},
		{"--project", "aginctus", "--confirm-create", "--force"},
	} {
		if _, err := parseOperatorProjectEnsureArgs(args); err == nil {
			t.Errorf("accepted incomplete or unsafe project-ensure args: %q", args)
		}
	}
	for _, args := range [][]string{
		{"--project", "aginctus", "--confirm-create"},
		{"--project=aginctus", "--confirm-create"},
	} {
		scope, err := parseOperatorProjectEnsureArgs(args)
		if err != nil || scope.Name != "aginctus" {
			t.Errorf("rejected valid explicit selection %q: scope=%v err=%v", args, scope, err)
		}
	}
}

func TestOperatorProjectEnsureFailClosed(t *testing.T) {
	scope, err := incusscope.NewProjectScope("aginctus")
	if err != nil {
		t.Fatal(err)
	}
	encode := func(projects []observedProject) []byte {
		t.Helper()
		data, err := json.Marshal(projects)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	owned := encode([]observedProject{{Name: scope.Name, Config: scope.DesiredConfig()}})
	foreignPolicy := scope.DesiredConfig()
	foreignPolicy["user.aginctus.resource"] = "someone-elses-project"
	foreign := encode([]observedProject{{Name: scope.Name, Config: foreignPolicy}})
	driftPolicy := scope.DesiredConfig()
	driftPolicy["features.networks"] = "true"
	drift := encode([]observedProject{{Name: scope.Name, Config: driftPolicy}})
	invisible := []byte("[]")
	cases := []struct {
		name          string
		first, second []byte
		readError     error
		secondError   error
		createError   error
		wantCode      int
		wantCreates   int
		wantReads     int
		wantResult    string
	}{
		{"compatible existing no mutation", owned, nil, nil, nil, nil, 0, 0, 1, "existing"},
		{"foreign existing never adopted", foreign, nil, nil, nil, nil, 1, 0, 1, ""},
		{"policy drift not repaired", drift, nil, nil, nil, nil, 1, 0, 1, ""},
		{"missing candidate created and verified", invisible, owned, nil, nil, nil, 0, 1, 2, "created"},
		{"collision rejected by atomic create", invisible, nil, nil, nil, errors.New("existing foreign project; secret"), 1, 1, 1, ""},
		{"post-create foreign result rejected", invisible, foreign, nil, nil, nil, 1, 1, 2, ""},
		{"post-create policy drift rejected", invisible, drift, nil, nil, nil, 1, 1, 2, ""},
		{"post-create invisible rejected", invisible, invisible, nil, nil, nil, 1, 1, 2, ""},
		{"unreadable initial enumeration", nil, nil, errors.New("private token"), nil, nil, 1, 0, 1, ""},
		{"unreadable post-create enumeration", invisible, nil, nil, errors.New("private token"), nil, 1, 1, 2, ""},
		{"malformed initial enumeration", []byte("null"), nil, nil, nil, nil, 1, 0, 1, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oldRead, oldCreate := inspectLocalProjects, createLocalProject
			t.Cleanup(func() { inspectLocalProjects, createLocalProject = oldRead, oldCreate })
			reads, creates := 0, 0
			inspectLocalProjects = func(context.Context) ([]byte, error) {
				reads++
				if reads == 1 {
					return tc.first, tc.readError
				}
				if reads == 2 {
					return tc.second, tc.secondError
				}
				t.Fatal("unexpected third project enumeration")
				return nil, nil
			}
			createLocalProject = func(_ context.Context, selected incusscope.ProjectScope) error {
				creates++
				if selected.Name != "aginctus" {
					t.Fatalf("wrong provision target: %s", selected.Name)
				}
				return tc.createError
			}
			var out, errOut bytes.Buffer
			code := runOperatorProjectEnsure(context.Background(), []string{"--project=aginctus", "--confirm-create"}, &out, &errOut)
			if code != tc.wantCode || reads != tc.wantReads || creates != tc.wantCreates {
				t.Fatalf("code=%d reads=%d creates=%d, expected code=%d reads=%d creates=%d; stderr=%q", code, reads, creates, tc.wantCode, tc.wantReads, tc.wantCreates, errOut.String())
			}
			if tc.wantResult != "" {
				var result operatorProjectEnsureReport
				if err := json.Unmarshal(out.Bytes(), &result); err != nil {
					t.Fatalf("success output is not JSON: %q: %v", out.String(), err)
				}
				if result.Project != "aginctus" || result.Result != tc.wantResult || result.Status != "compatible" {
					t.Fatalf("unexpected ensure report: %+v", result)
				}
			} else if out.Len() != 0 {
				t.Fatalf("unsafe report on failure: %q", out.String())
			}
			if strings.Contains(out.String()+errOut.String(), "secret") || strings.Contains(out.String()+errOut.String(), "private token") {
				t.Fatal("external error leaked")
			}
		})
	}
}

func TestOperatorProjectEnsureNeverCreatesOnUnsafeArgs(t *testing.T) {
	oldRead, oldCreate := inspectLocalProjects, createLocalProject
	t.Cleanup(func() { inspectLocalProjects, createLocalProject = oldRead, oldCreate })
	inspectLocalProjects = func(context.Context) ([]byte, error) {
		t.Fatal("read reached with unsafe arguments")
		return nil, nil
	}
	createLocalProject = func(context.Context, incusscope.ProjectScope) error {
		t.Fatal("create reached with unsafe arguments")
		return nil
	}
	for _, args := range [][]string{nil, {"--project", "aginctus"}, {"--project", "default", "--confirm-create"}} {
		var out, errOut bytes.Buffer
		if code := runOperatorProjectEnsure(context.Background(), args, &out, &errOut); code != 2 || out.Len() != 0 {
			t.Fatalf("args=%q code=%d stdout=%q", args, code, out.String())
		}
	}
}

func TestOperatorProjectEnsureRejectsConfigOverridesBeforeLoading(t *testing.T) {
	for _, args := range [][]string{
		{"--configuration-file=/missing", "operator", "project-ensure", "--project", "aginctus", "--confirm-create"},
		{"--config=gateway.id=other", "operator", "project-ensure", "--project=aginctus", "--confirm-create"},
	} {
		var out, errOut bytes.Buffer
		if code := Run(context.Background(), args, &out, &errOut, nil, nil); code != 2 ||
			out.Len() != 0 || !strings.Contains(errOut.String(), "configuration overrides are not supported") {
			t.Fatalf("args=%q stdout=%q stderr=%q code=%d", args, out.String(), errOut.String(), code)
		}
	}
}
