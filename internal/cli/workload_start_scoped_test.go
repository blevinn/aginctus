package cli

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

func scopedStartLoader() *config.Loader {
	loader := config.NewLoader()
	loader.SystemPath, loader.UserPath, loader.Environment = "", "", nil
	return loader
}

func stubScopedStartEnvironment(t *testing.T) (status *string, projectReads *int, instanceReads *int, unitReads *int, starts *int) {
	t.Helper()
	oldProjects, oldInstances, oldLegacy := inspectLocalProjects, inspectScopedInstances, inspectIncusInstances
	oldUnits, oldLease, oldStart := inspectOperatorUnit, acquireScopedImageMutationLease, startScopedWorkloadIncus
	t.Cleanup(func() {
		inspectLocalProjects, inspectScopedInstances, inspectIncusInstances = oldProjects, oldInstances, oldLegacy
		inspectOperatorUnit, acquireScopedImageMutationLease, startScopedWorkloadIncus = oldUnits, oldLease, oldStart
	})
	state := "Stopped"
	projectCalls, instanceCalls, unitCalls, startCalls := 0, 0, 0, 0
	inspectLocalProjects = func(context.Context) ([]byte, error) {
		projectCalls++
		return []byte(scopedStatusOwnedProject), nil
	}
	inspectScopedInstances = func(_ context.Context, scope incusscope.ProjectScope) ([]byte, error) {
		instanceCalls++
		if scope.Name != "aginctus" {
			t.Fatalf("wrong project inventory: %q", scope.Name)
		}
		return []byte("[" + strings.Replace(scopedStatusWorkload, "Running", state, 1) + "]"), nil
	}
	inspectIncusInstances = func(context.Context) ([]byte, error) {
		t.Fatal("scoped start must not use legacy all-projects inventory")
		return nil, nil
	}
	inspectOperatorUnit = func(_ context.Context, manager, unit string) ([]byte, error) {
		unitCalls++
		if manager != "system" && manager != "user" {
			t.Fatalf("unexpected systemd manager %q", manager)
		}
		found := false
		for _, known := range operatorLifecycleUnits {
			if known == unit {
				found = true
			}
		}
		if !found {
			t.Fatalf("unexpected lifecycle unit %q", unit)
		}
		return []byte("LoadState=not-found\nActiveState=inactive\nUnitFileState=\n"), nil
	}
	acquireScopedImageMutationLease = func(scope incusscope.ProjectScope) (func(), error) {
		if scope.Name != "aginctus" {
			t.Fatalf("wrong lease scope: %q", scope.Name)
		}
		return func() {}, nil
	}
	startScopedWorkloadIncus = func(_ context.Context, scope incusscope.ProjectScope, name string) error {
		startCalls++
		if scope.Name != "aginctus" || name != "aginctus-dev" {
			t.Fatalf("unexpected scoped mutation: %q %q", scope.Name, name)
		}
		state = "Running"
		return nil
	}
	return &state, &projectCalls, &instanceCalls, &unitCalls, &startCalls
}

func TestScopedWorkloadStartRequiresExplicitProjectAndConfirmation(t *testing.T) {
	scope, _ := incusscope.NewProjectScope("aginctus")
	if got, want := scopedWorkloadStartArgs(scope, "aginctus-dev"), []string{"--force-local", "--project", "aginctus", "start", "aginctus-dev"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unsafe Incus arguments %q, want %q", got, want)
	}
	loader := scopedStartLoader()
	_, _, _, _, starts := stubScopedStartEnvironment(t)
	for _, args := range [][]string{
		{"workload", "start", "--project", "aginctus", "dev"},
		{"workload", "start", "--project", "default", "dev", "--confirm-start"},
		{"workload", "start", "--project", "other", "dev", "--confirm-start"},
		{"workload", "start", "--project", "aginctus", "--confirm-start"},
		{"workload", "start", "--project", "aginctus", "dev", "--confirm-start", "--force"},
		{"workload", "start", "--project=../escape", "dev", "--confirm-start"},
		{"workload", "start", "--project", "aginctus", "missing", "--confirm-start"},
	} {
		var out, errOut bytes.Buffer
		code := Run(context.Background(), args, &out, &errOut, &fakeIncusClient{}, loader)
		if code == 0 || out.Len() != 0 || *starts != 0 {
			t.Fatalf("accepted unsafe args %q: code=%d out=%q err=%q writes=%d", args, code, out.String(), errOut.String(), *starts)
		}
	}
}

func TestScopedWorkloadStartSingleOwnedInstance(t *testing.T) {
	loader := scopedStartLoader()
	state, projects, reads, units, starts := stubScopedStartEnvironment(t)
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"workload", "start", "--project=aginctus", "dev", "--confirm-start"}, &out, &errOut, &fakeIncusClient{}, loader)
	if code != 0 || out.String() != "workload \"dev\": started in project \"aginctus\"\n" || errOut.Len() != 0 {
		t.Fatalf("start: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	if *state != "Running" || *starts != 1 || *reads != 3 || *projects != 3 || *units != 2*len(operatorLifecycleUnits)*2 {
		t.Fatalf("checks: state=%s writes=%d instanceReads=%d projectReads=%d unitReads=%d", *state, *starts, *reads, *projects, *units)
	}
	out.Reset()
	code = Run(context.Background(), []string{"workload", "start", "--project", "aginctus", "dev", "--confirm-start"}, &out, &errOut, &fakeIncusClient{}, loader)
	if code != 0 || !strings.Contains(out.String(), "already running") || *starts != 1 {
		t.Fatalf("idempotent start: code=%d out=%q writes=%d", code, out.String(), *starts)
	}
}

func TestScopedWorkloadStartRejectsUnverifiedStateWithoutMutation(t *testing.T) {
	for _, mode := range []string{
		"foreign", "missing", "unknown", "project drift", "controller present", "controller unknown",
		"controller changed", "state changed", "lease denied", "inventory denied",
	} {
		t.Run(mode, func(t *testing.T) {
			loader := scopedStartLoader()
			state, projects, reads, units, starts := stubScopedStartEnvironment(t)
			switch mode {
			case "foreign":
				inspectScopedInstances = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
					return []byte(`[{"name":"aginctus-dev","status":"Stopped","config":{}}` + "]"), nil
				}
			case "missing":
				inspectScopedInstances = func(context.Context, incusscope.ProjectScope) ([]byte, error) { return []byte("[]"), nil }
			case "unknown":
				*state = "Frozen"
			case "project drift":
				inspectLocalProjects = func(context.Context) ([]byte, error) {
					*projects++
					if *projects > 1 {
						return []byte("[]"), nil
					}
					return []byte(scopedStatusOwnedProject), nil
				}
			case "controller present", "controller unknown", "controller changed":
				inspectOperatorUnit = func(context.Context, string, string) ([]byte, error) {
					*units++
					if mode == "controller present" {
						return []byte("LoadState=loaded\nActiveState=active\nUnitFileState=enabled\n"), nil
					}
					if mode == "controller unknown" || *units > 2*len(operatorLifecycleUnits) {
						return nil, errors.New("sensitive systemd error")
					}
					return []byte("LoadState=not-found\nActiveState=inactive\nUnitFileState=\n"), nil
				}
			case "state changed":
				inspectScopedInstances = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
					*reads++
					if *reads >= 2 {
						return []byte("[" + scopedStatusWorkload + "]"), nil
					}
					return []byte("[" + strings.Replace(scopedStatusWorkload, "Running", "Stopped", 1) + "]"), nil
				}
			case "lease denied":
				acquireScopedImageMutationLease = func(incusscope.ProjectScope) (func(), error) {
					return nil, errors.New("sensitive socket path")
				}
			case "inventory denied":
				inspectScopedInstances = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
					return nil, errors.New("sensitive remote output")
				}
			}
			var out, errOut bytes.Buffer
			code := Run(context.Background(), []string{"workload", "start", "--project", "aginctus", "dev", "--confirm-start"}, &out, &errOut, &fakeIncusClient{}, loader)
			if code != 1 || out.Len() != 0 || *starts != 0 || strings.Contains(errOut.String(), "sensitive") {
				t.Fatalf("unsafe %s: code=%d out=%q err=%q writes=%d", mode, code, out.String(), errOut.String(), *starts)
			}
		})
	}
}

func TestScopedWorkloadStartPostflightFailureNotReportedAsSuccess(t *testing.T) {
	loader := scopedStartLoader()
	_, _, _, _, starts := stubScopedStartEnvironment(t)
	startScopedWorkloadIncus = func(context.Context, incusscope.ProjectScope, string) error {
		*starts++
		return nil // Daemon returned success, but instance never became running.
	}
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"workload", "start", "--project", "aginctus", "dev", "--confirm-start"}, &out, &errOut, &fakeIncusClient{}, loader)
	if code != 1 || out.Len() != 0 || *starts != 1 || !strings.Contains(errOut.String(), "not verified") {
		t.Fatalf("false success: code=%d out=%q err=%q writes=%d", code, out.String(), errOut.String(), *starts)
	}
}
