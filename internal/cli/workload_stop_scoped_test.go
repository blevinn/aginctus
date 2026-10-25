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

func stubScopedStopEnvironment(t *testing.T) (status *string, projectReads, instanceReads, unitReads, stops *int) {
	t.Helper()
	status, projectReads, instanceReads, unitReads, _ = stubScopedStartEnvironment(t)
	*status = "Running"
	originalStop, originalLegacy := stopScopedWorkloadIncus, runWorkloadIncus
	t.Cleanup(func() {
		stopScopedWorkloadIncus, runWorkloadIncus = originalStop, originalLegacy
	})
	runWorkloadIncus = func(context.Context, ...string) error {
		t.Fatal("scoped stop attempted legacy default-project mutation")
		return nil
	}
	calls := 0
	stopScopedWorkloadIncus = func(_ context.Context, scope incusscope.ProjectScope, instance string) error {
		calls++
		if scope.Name != "aginctus" || instance != "aginctus-dev" {
			t.Fatalf("unexpected scoped stop target: %q %q", scope.Name, instance)
		}
		*status = "Stopped"
		return nil
	}
	return status, projectReads, instanceReads, unitReads, &calls
}

func invokeScopedStop(t *testing.T, loader *config.Loader, arguments ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), arguments, &stdout, &stderr, &fakeIncusClient{}, loader)
	return code, stdout.String(), stderr.String()
}

func TestScopedWorkloadStopExactArgsAndSingleTarget(t *testing.T) {
	scope, _ := incusscope.NewProjectScope("aginctus")
	want := []string{"--force-local", "--project", "aginctus", "stop", "aginctus-dev"}
	if got := scopedWorkloadStopArgs(scope, "aginctus-dev"); !reflect.DeepEqual(got, want) {
		t.Fatalf("unsafe scoped stop command: got=%q want=%q", got, want)
	}
	loader := scopedStartLoader()
	state, projects, inventory, unitChecks, writes := stubScopedStopEnvironment(t)
	code, out, stderr := invokeScopedStop(t, loader, "workload", "stop", "--project=aginctus", "dev", "--confirm-stop")
	if code != 0 || out != "workload \"dev\": stopped in project \"aginctus\"\n" || stderr != "" {
		t.Fatalf("stop: code=%d stdout=%q stderr=%q", code, out, stderr)
	}
	if *state != "Stopped" || *writes != 1 || *projects != 3 || *inventory != 3 || *unitChecks != 4*len(operatorLifecycleUnits) {
		t.Fatalf("checks: state=%s writes=%d projects=%d inventory=%d units=%d", *state, *writes, *projects, *inventory, *unitChecks)
	}
	code, out, stderr = invokeScopedStop(t, loader, "workload", "stop", "--project", "aginctus", "dev", "--confirm-stop")
	if code != 0 || !strings.Contains(out, "already stopped") || stderr != "" || *writes != 1 {
		t.Fatalf("idempotent stop: code=%d stdout=%q stderr=%q writes=%d", code, out, stderr, *writes)
	}
}

func TestScopedWorkloadStopRefusesUnconfirmedOrUnscopedAction(t *testing.T) {
	loader := scopedStartLoader()
	_, projects, _, _, writes := stubScopedStopEnvironment(t)
	for _, args := range [][]string{
		{"workload", "stop", "--project", "aginctus", "dev"},
		{"workload", "stop", "--project=aginctus", "dev", "--confirm-start"},
		{"workload", "stop", "--project", "aginctus", "--confirm-stop"},
		{"workload", "stop", "--project", "aginctus", "dev", "--confirm-stop", "--force"},
		{"workload", "stop", "--project", "default", "dev", "--confirm-stop"},
		{"workload", "stop", "--project=../escape", "dev", "--confirm-stop"},
		{"workload", "stop", "--project", "other", "dev", "--confirm-stop"},
		{"workload", "stop", "--project", "aginctus", "missing", "--confirm-stop"},
	} {
		code, out, stderr := invokeScopedStop(t, loader, args...)
		if code == 0 || out != "" || stderr == "" || *writes != 0 {
			t.Fatalf("accepted unsafe args %q: code=%d stdout=%q stderr=%q writes=%d", args, code, out, stderr, *writes)
		}
	}
	if *projects != 0 {
		t.Fatalf("malformed or mismatched project contacted Incus %d times", *projects)
	}
}

func TestScopedWorkloadStopFailsClosedBeforeMutation(t *testing.T) {
	for _, mode := range []string{
		"missing", "foreign", "unexpected state", "project drift", "inventory denied",
		"controller present", "controller unknown", "controller changed",
		"instance changed", "lease denied",
	} {
		t.Run(mode, func(t *testing.T) {
			loader := scopedStartLoader()
			state, projects, inventory, units, writes := stubScopedStopEnvironment(t)
			switch mode {
			case "missing":
				inspectScopedInstances = func(context.Context, incusscope.ProjectScope) ([]byte, error) { return []byte("[]"), nil }
			case "foreign":
				inspectScopedInstances = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
					return []byte(`[{"name":"aginctus-dev","status":"Running","config":{}}]`), nil
				}
			case "unexpected state":
				*state = "Frozen"
			case "project drift":
				inspectLocalProjects = func(context.Context) ([]byte, error) {
					*projects++
					if *projects > 1 {
						return []byte("[]"), nil
					}
					return []byte(scopedStatusOwnedProject), nil
				}
			case "inventory denied":
				inspectScopedInstances = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
					return nil, errors.New("sensitive incus output")
				}
			case "controller present", "controller unknown", "controller changed":
				inspectOperatorUnit = func(context.Context, string, string) ([]byte, error) {
					*units++
					if mode == "controller present" {
						return []byte("LoadState=loaded\nActiveState=active\nUnitFileState=enabled\n"), nil
					}
					if mode == "controller unknown" || *units > 2*len(operatorLifecycleUnits) {
						return nil, errors.New("sensitive systemctl error")
					}
					return []byte("LoadState=not-found\nActiveState=inactive\nUnitFileState=\n"), nil
				}
			case "instance changed":
				inspectScopedInstances = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
					*inventory++
					status := "Running"
					if *inventory > 1 {
						status = "Stopped"
					}
					return []byte("[" + strings.Replace(scopedStatusWorkload, "Running", status, 1) + "]"), nil
				}
			case "lease denied":
				acquireScopedImageMutationLease = func(incusscope.ProjectScope) (func(), error) {
					return nil, errors.New("sensitive lease error")
				}
			}
			code, out, stderr := invokeScopedStop(t, loader, "workload", "stop", "--project", "aginctus", "dev", "--confirm-stop")
			if code != 1 || out != "" || *writes != 0 || strings.Contains(stderr, "sensitive") {
				t.Fatalf("%s did not fail closed: code=%d stdout=%q stderr=%q writes=%d", mode, code, out, stderr, *writes)
			}
		})
	}
}

func TestScopedWorkloadStopUnverifiedPostflightAndDaemonFailure(t *testing.T) {
	for _, mode := range []string{"daemon failure", "postflight still running", "postflight ownership changed"} {
		t.Run(mode, func(t *testing.T) {
			loader := scopedStartLoader()
			_, _, inventory, _, writes := stubScopedStopEnvironment(t)
			stopScopedWorkloadIncus = func(context.Context, incusscope.ProjectScope, string) error {
				*writes++
				if mode == "daemon failure" {
					return errors.New("sensitive daemon response")
				}
				return nil
			}
			if mode == "postflight ownership changed" {
				inspectScopedInstances = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
					*inventory++
					if *inventory == 3 {
						return []byte(`[{"name":"aginctus-dev","status":"Stopped","config":{}}]`), nil
					}
					return []byte("[" + scopedStatusWorkload + "]"), nil
				}
			}
			code, out, stderr := invokeScopedStop(t, loader, "workload", "stop", "--project", "aginctus", "dev", "--confirm-stop")
			if code != 1 || out != "" || *writes != 1 || strings.Contains(stderr, "sensitive") {
				t.Fatalf("%s returned false success: code=%d stdout=%q stderr=%q writes=%d", mode, code, out, stderr, *writes)
			}
		})
	}
}

func TestScopedWorkloadStopHelpAvailableOffline(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"help", "workload"}, {"workload", "--help"}} {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), args, &stdout, &stderr, nil, nil)
		if code != 0 || stderr.Len() != 0 ||
			!strings.Contains(stdout.String(), "workload stop --project NAME WORKLOAD-ID --confirm-stop") {
			t.Fatalf("scoped stop missing from offline help: args=%q code=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}
}
