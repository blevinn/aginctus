package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

func TestImageReplacePlanRejectsInvalidArgumentsBeforeIncus(t *testing.T) {
	cfg, err := (&config.Loader{Environment: nil}).Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	oldProjects, oldImages, oldUnits, oldLegacy := inspectLocalProjects, inspectScopedImages, inspectOperatorUnit, runIncusImage
	t.Cleanup(func() {
		inspectLocalProjects, inspectScopedImages, inspectOperatorUnit, runIncusImage = oldProjects, oldImages, oldUnits, oldLegacy
	})
	inspectLocalProjects = func(context.Context) ([]byte, error) { t.Fatal("invalid input contacted Incus"); return nil, nil }
	inspectScopedImages = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
		t.Fatal("invalid input inspected images")
		return nil, nil
	}
	inspectOperatorUnit = func(context.Context, string, string) ([]byte, error) {
		t.Fatal("invalid input inspected systemd")
		return nil, nil
	}
	runIncusImage = func(context.Context, ...string) ([]byte, error) {
		t.Fatal("ambient image operation")
		return nil, nil
	}
	for _, args := range [][]string{
		{"replace-plan"},
		{"replace-plan", "--project", "aginctus", "--owner", "system", "x"},
		{"replace-plan", "--project", "default", "--owner", "system", "x", "/tmp"},
		{"replace-plan", "--project", "other", "--owner", "system", "x", "/tmp"},
		{"replace-plan", "--project", "aginctus", "--owner", "root", "x", "/tmp"},
		{"replace-plan", "--project", "aginctus", "--owner", "user", "../bad", "/tmp"},
		{"replace-plan", "--project", "aginctus", "--owner", "user", "x", ""},
		{"replace-plan", "--project=aginctus", "--owner=system", "x", "/tmp"},
		{"replace-plan", "--project", "aginctus", "--owner", "system", "x", "/tmp", "--force"},
	} {
		var out, errOut bytes.Buffer
		if code := runConfiguredImages(context.Background(), args, &out, &errOut, cfg); code != 2 || out.Len() != 0 {
			t.Fatalf("%q: code=%d stdout=%q stderr=%q", args, code, out.String(), errOut.String())
		}
	}
}

func TestImageReplacementPlanClassifiesStateAndControllers(t *testing.T) {
	dir, desired := scopedImportFixture(t)
	scope, _ := incusscope.NewProjectScope("aginctus")
	oldFP := strings.Repeat("b", 64)
	claim := expectedImageOwnerClaim(scope, "my-image", "system")
	current := ownedImageFixture(oldFP, []string{"my-image"}, claim)
	upToDate := ownedImageFixture(desired, []string{"my-image"}, claim)
	unmarked := ownedImageFixture(oldFP, []string{"my-image"}, nil)
	shared := ownedImageFixture(oldFP, []string{"my-image", "other"}, claim)
	otherImage := ownedImageFixture(desired, []string{"other"}, nil)
	presentContent := []byte("[" + strings.Trim(string(current), "[]") + "," + strings.Trim(string(otherImage), "[]") + "]")
	ownerInstalled := fakeOperatorUnitProperties("loaded", "inactive", "disabled")
	otherAbsent := fakeOperatorUnitProperties("not-found", "inactive", "")
	otherInstalled := fakeOperatorUnitProperties("loaded", "inactive", "enabled")
	unknown := []byte("broken manager response")
	cfg, err := (&config.Loader{Environment: nil}).Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name          string
		projects      string
		projectErr    error
		inventory     []byte
		inventoryErr  error
		systemUnit    []byte
		userUnit      []byte
		status        string
		code          int
		unitReads     int
	}{
		{name: "candidate but never authorized", projects: auditOwnedProject, inventory: current,
			systemUnit: ownerInstalled, userUnit: otherAbsent, status: "candidate-requires-authorization", unitReads: 8},
		{name: "already desired", projects: auditOwnedProject, inventory: upToDate, status: "up-to-date"},
		{name: "missing alias", projects: auditOwnedProject, inventory: []byte("[]"), status: "blocked-absent", code: 1},
		{name: "unmarked alias", projects: auditOwnedProject, inventory: unmarked, status: "blocked-unmarked", code: 1},
		{name: "shared alias", projects: auditOwnedProject, inventory: shared, status: "blocked-shared", code: 1},
		{name: "target exists elsewhere", projects: auditOwnedProject, inventory: presentContent, status: "blocked-target-fingerprint-present", code: 1},
		{name: "competing user unit", projects: auditOwnedProject, inventory: current,
			systemUnit: ownerInstalled, userUnit: otherInstalled, status: "blocked-competing-controller", code: 1, unitReads: 8},
		{name: "missing selected owner unit", projects: auditOwnedProject, inventory: current,
			systemUnit: otherAbsent, userUnit: otherAbsent, status: "blocked-controller-unverified", code: 1, unitReads: 8},
		{name: "unverified other manager", projects: auditOwnedProject, inventory: current,
			systemUnit: ownerInstalled, userUnit: unknown, status: "blocked-controller-unverified", code: 1, unitReads: 8},
		{name: "unavailable project", projects: "[]", code: 1},
		{name: "foreign project", projects: strings.Replace(auditOwnedProject, "deployment-project", "other", 1), code: 1},
		{name: "project denied", projectErr: errors.New("private-project-info"), code: 1},
		{name: "inventory denied", projects: auditOwnedProject, inventoryErr: errors.New("private-image-info"), code: 1},
		{name: "invalid image JSON", projects: auditOwnedProject, inventory: []byte("null"), code: 1},
		{name: "oversized image listing", projects: auditOwnedProject, inventory: bytes.Repeat([]byte("x"), maxProjectListBytes+1), code: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			oldProjects, oldImages, oldUnits, oldLegacy := inspectLocalProjects, inspectScopedImages, inspectOperatorUnit, runIncusImage
			t.Cleanup(func() {
				inspectLocalProjects, inspectScopedImages, inspectOperatorUnit, runIncusImage = oldProjects, oldImages, oldUnits, oldLegacy
			})
			inspectLocalProjects = func(context.Context) ([]byte, error) { return []byte(tc.projects), tc.projectErr }
			reads := 0
			inspectScopedImages = func(_ context.Context, selected incusscope.ProjectScope) ([]byte, error) {
				if selected.Name != scope.Name {
					t.Fatalf("unexpected scope %q", selected.Name)
				}
				return tc.inventory, tc.inventoryErr
			}
			inspectOperatorUnit = func(_ context.Context, manager, unit string) ([]byte, error) {
				reads++
				if !strings.HasSuffix(unit, ".service") {
					t.Fatalf("unexpected unit %q", unit)
				}
				if manager == "system" {
					return tc.systemUnit, nil
				}
				if manager == "user" {
					return tc.userUnit, nil
				}
				t.Fatalf("unexpected manager %q", manager)
				return nil, nil
			}
			runIncusImage = func(context.Context, ...string) ([]byte, error) {
				t.Fatal("ambient or mutating image runner reached")
				return nil, nil
			}
			var out, errOut bytes.Buffer
			code := Run(context.Background(), []string{
				"images", "replace-plan", "--project", "aginctus", "--owner", "system", "my-image", dir,
			}, &out, &errOut, nil, &fakeConfigLoader{cfg: cfg})
			if code != tc.code || reads != tc.unitReads {
				t.Fatalf("code=%d unitReads=%d want=%d/%d stdout=%q stderr=%q",
					code, reads, tc.code, tc.unitReads, out.String(), errOut.String())
			}
			if strings.Contains(out.String()+errOut.String(), "private-") {
				t.Fatal("raw subprocess error leaked")
			}
			if tc.status == "" {
				if out.Len() != 0 {
					t.Fatalf("unverified state produced a plan: %q", out.String())
				}
				return
			}
			var plan imageReplacePlanReport
			if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
				t.Fatalf("invalid plan JSON %q: %v", out.String(), err)
			}
			if plan.WriteAuthorized || plan.Status != tc.status || plan.Project != scope.Name ||
				plan.Alias != "my-image" || plan.Owner != "system" || plan.DesiredFingerprint != desired {
				t.Fatalf("bad read-only plan: %+v", plan)
			}
			if tc.status == "candidate-requires-authorization" && (plan.CurrentFingerprint != oldFP ||
				plan.KnownControllers != "no-known-conflict") {
				t.Fatalf("candidate missing evidence: %+v", plan)
			}
		})
	}
}

func TestImageReplacementPlanOwnerSymmetry(t *testing.T) {
	missing := fakeOperatorUnitProperties("not-found", "inactive", "")
	present := fakeOperatorUnitProperties("loaded", "active", "enabled")
	before := inspectOperatorUnit
	t.Cleanup(func() { inspectOperatorUnit = before })
	inspectOperatorUnit = func(_ context.Context, manager, _ string) ([]byte, error) {
		if manager == "system" {
			return missing, nil
		}
		return present, nil
	}
	if got := surveyImageControllers(context.Background(), "user"); got != "no-known-conflict" {
		t.Fatalf("current user manager should be observed: %q", got)
	}
	if got := surveyImageControllers(context.Background(), "system"); got != "competing-known-unit" {
		t.Fatalf("system controller conflict must be reported: %q", got)
	}
}
