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

func scopedApplyRenderFixture(t *testing.T) (*config.Loader, *int, *int) {
	t.Helper()
	loader := config.NewLoader()
	loader.SystemPath, loader.UserPath, loader.Environment = "", "", nil
	oldProjects, oldScoped, oldLegacy, oldWrite := inspectLocalProjects, inspectScopedInstances, inspectIncusInstances, runWorkloadIncus
	t.Cleanup(func() {
		inspectLocalProjects, inspectScopedInstances, inspectIncusInstances, runWorkloadIncus = oldProjects, oldScoped, oldLegacy, oldWrite
	})
	projectReads, instanceReads := 0, 0
	inspectLocalProjects = func(context.Context) ([]byte, error) {
		projectReads++
		return []byte(scopedStatusOwnedProject), nil
	}
	inspectScopedInstances = func(_ context.Context, scope incusscope.ProjectScope) ([]byte, error) {
		instanceReads++
		if scope.Name != "aginctus" {
			t.Fatalf("inspected unexpected project %q", scope.Name)
		}
		return []byte("[" + scopedStatusWorkload + "]"), nil
	}
	inspectIncusInstances = func(context.Context) ([]byte, error) {
		t.Fatal("scoped preview used legacy all-projects inventory")
		return nil, nil
	}
	runWorkloadIncus = func(context.Context, ...string) error {
		t.Fatal("scoped preview used legacy Incus mutation")
		return nil
	}
	return loader, &projectReads, &instanceReads
}

func renderScopedApply(t *testing.T, loader *config.Loader, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Run(context.Background(), args, &out, &errOut, &fakeIncusClient{}, loader)
	return code, out.String(), errOut.String()
}

func TestScopedWorkloadApplyRenderOnlyOneProjectBoundInstance(t *testing.T) {
	loader, projects, reads := scopedApplyRenderFixture(t)
	for _, flag := range [][]string{
		{"--project", "aginctus"}, {"--project=aginctus"},
	} {
		args := append([]string{"workload", "apply"}, flag...)
		args = append(args, "dev", "--render")
		code, output, stderr := renderScopedApply(t, loader, args...)
		if code != 0 || stderr != "" {
			t.Fatalf("render %q: code=%d stdout=%q stderr=%q", args, code, output, stderr)
		}
		var report struct {
			Kind string `json:"kind"`
			Project string `json:"project"`
			WorkloadID string `json:"workloadId"`
			ObservedState string `json:"observedState"`
			WriteAuthorized bool `json:"writeAuthorized"`
			Plan struct {
				Steps []struct {
					ID string `json:"id"`
					Driver string `json:"driver"`
					Configuration struct {
						Project string `json:"project"`
						Operation string `json:"operation"`
						RequireExistingConfig map[string]string `json:"requireExistingConfig"`
						Documents []struct {
							Kind string `json:"kind"`
							Name string `json:"name"`
						} `json:"documents"`
					} `json:"configuration"`
				} `json:"steps"`
			} `json:"plan"`
		}
		if err := json.Unmarshal([]byte(output), &report); err != nil {
			t.Fatal(err)
		}
		if report.Kind != "ScopedWorkloadApplyPreview" || report.Project != "aginctus" ||
			report.WorkloadID != "dev" || report.ObservedState != "Running" ||
			report.WriteAuthorized || len(report.Plan.Steps) != 1 {
			t.Fatalf("unsafe preview report: %#v", report)
		}
		step := report.Plan.Steps[0]
		if step.ID != "workload-dev" || step.Driver != "apply" ||
			step.Configuration.Project != "aginctus" || step.Configuration.Operation != "upsert" ||
			step.Configuration.RequireExistingConfig["user.aginctus.role"] != "agent-workload" ||
			len(step.Configuration.Documents) != 1 ||
			step.Configuration.Documents[0].Kind != "instance" ||
			step.Configuration.Documents[0].Name != "aginctus-dev" {
			t.Fatalf("unexpected workload-only step: %#v", step)
		}
		if strings.Contains(output, "\"kind\":\"network\"") ||
			strings.Contains(output, "\"writeAuthorized\":true") {
			t.Fatalf("scoped preview attempted host network management: %s", output)
		}
	}
	if *projects != 2 || *reads != 2 {
		t.Fatalf("reads: projects=%d instances=%d", *projects, *reads)
	}
}

func TestScopedWorkloadApplyRenderRefusesMutatingSyntax(t *testing.T) {
	loader, projects, reads := scopedApplyRenderFixture(t)
	for _, args := range [][]string{
		{"workload", "apply", "--project", "aginctus", "dev"},
		{"workload", "apply", "--project", "aginctus", "dev", "--dry-run"},
		{"workload", "apply", "--project", "aginctus", "dev", "--force"},
		{"workload", "apply", "--project", "aginctus", "dev", "--confirm-apply"},
		{"workload", "apply", "--project", "aginctus", "--render"},
		{"workload", "apply", "--project", "aginctus", "dev", "--render", "--force"},
		{"workload", "apply", "--project", "default", "dev", "--render"},
		{"workload", "apply", "--project", "../escape", "dev", "--render"},
		{"workload", "apply", "--project", "other", "dev", "--render"},
		{"workload", "apply", "--project", "aginctus", "missing", "--render"},
	} {
		code, out, stderr := renderScopedApply(t, loader, args...)
		if code == 0 || out != "" || stderr == "" {
			t.Fatalf("unsafe syntax %q accepted: code=%d stdout=%q stderr=%q", args, code, out, stderr)
		}
	}
	if *projects != 0 || *reads != 0 {
		t.Fatalf("invalid syntax contacted Incus: projects=%d reads=%d", *projects, *reads)
	}
}

func TestScopedWorkloadApplyRenderRequiresPresentOwnedInstance(t *testing.T) {
	for _, mode := range []string{"absent", "foreign", "unknown-state", "wrong-project", "duplicate", "denied", "project-denied"} {
		t.Run(mode, func(t *testing.T) {
			loader, _, _ := scopedApplyRenderFixture(t)
			switch mode {
			case "absent":
				inspectScopedInstances = func(context.Context, incusscope.ProjectScope) ([]byte, error) { return []byte("[]"), nil }
			case "foreign":
				inspectScopedInstances = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
					return []byte(`[{"name":"aginctus-dev","status":"Running","config":{}}]`), nil
				}
			case "unknown-state":
				inspectScopedInstances = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
					return []byte("[" + strings.Replace(scopedStatusWorkload, "Running", "Frozen", 1) + "]"), nil
				}
			case "wrong-project":
				inspectScopedInstances = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
					return []byte("[" + strings.Replace(scopedStatusWorkload, "\"project\":\"aginctus\"", "\"project\":\"default\"", 1) + "]"), nil
				}
			case "duplicate":
				inspectScopedInstances = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
					return []byte("[" + scopedStatusWorkload + "," + scopedStatusWorkload + "]"), nil
				}
			case "denied":
				inspectScopedInstances = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
					return nil, errors.New("sensitive remote instance output")
				}
			case "project-denied":
				inspectLocalProjects = func(context.Context) ([]byte, error) {
					return nil, errors.New("sensitive project listing")
				}
			}
			code, out, stderr := renderScopedApply(t, loader, "workload", "apply", "--project", "aginctus", "dev", "--render")
			if code != 1 || out != "" || strings.Contains(stderr, "sensitive") {
				t.Fatalf("%s: code=%d stdout=%q stderr=%q", mode, code, out, stderr)
			}
		})
	}
}

func TestScopedWorkloadApplyRenderHelpOffline(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"help", "workload"}, {"workload", "--help"}} {
		var out, errOut bytes.Buffer
		code := Run(context.Background(), args, &out, &errOut, nil, nil)
		if code != 0 || errOut.Len() != 0 ||
			!strings.Contains(out.String(), "workload apply --project NAME WORKLOAD-ID --render") {
			t.Fatalf("missing offline help for %q: code=%d stderr=%q", args, code, errOut.String())
		}
	}
}
