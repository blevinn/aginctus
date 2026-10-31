package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

func stubScopedNativeDiff(t *testing.T, answer native.InstancePlan, failure error) *int {
	t.Helper()
	original := planScopedExistingWorkloadDiff
	t.Cleanup(func() { planScopedExistingWorkloadDiff = original })
	calls := 0
	planScopedExistingWorkloadDiff = func(_ context.Context, intent native.InstanceIntent) (native.InstancePlan, error) {
		calls++
		if err := intent.Validate(); err != nil {
			t.Fatal(err)
		}
		if intent.Identity.Project != "aginctus" || intent.Identity.Name != "aginctus-dev" ||
			!intent.EnsureRunning || intent.WorkloadID != "dev" {
			t.Fatalf("unsafe native planner input")
		}
		return answer, failure
	}
	return &calls
}

func oneScopedDiff(action string, changes int) native.InstancePlan {
	plan := native.InstancePlan{Identity: native.InstanceIdentity{Project: "aginctus", Name: "aginctus-dev"}, Action: native.Action(action)}
	for i := 0; i < changes; i++ {
		plan.Changes = append(plan.Changes, native.FieldChange{Field: "config.secret"})
	}
	return plan
}

func TestScopedWorkloadApplyNativeDryRunSummarizesWithoutSecrets(t *testing.T) {
	for _, tc := range []struct {
		action  string
		changes int
		want    string
	}{
		{"unchanged", 0, "unchanged"},
		{"update", 2, "would-update"},
		{"update", 0, "would-update"}, // Planner may propose starting a stopped instance.
	} {
		t.Run(tc.action+"-changes-"+string(rune('0'+tc.changes)), func(t *testing.T) {
			loader, projects, reads := scopedApplyRenderFixture(t)
			calls := stubScopedNativeDiff(t, oneScopedDiff(tc.action, tc.changes), nil)
			for _, projectArgs := range [][]string{{"--project", "aginctus"}, {"--project=aginctus"}} {
				args := append([]string{"workload", "apply"}, projectArgs...)
				args = append(args, "dev", "--dry-run")
				code, output, diagnostic := renderScopedApply(t, loader, args...)
				if code != 0 || diagnostic != "" {
					t.Fatalf("dry-run %q: code=%d output=%q err=%q", args, code, output, diagnostic)
				}
				var report scopedWorkloadApplyDiff
				if err := json.Unmarshal([]byte(output), &report); err != nil {
					t.Fatal(err)
				}
				if report.Kind != "ScopedWorkloadApplyDiff" || report.Project != "aginctus" ||
					report.WorkloadID != "dev" || report.ObservedState != "Running" ||
					report.Action != tc.want || report.ChangeCount != tc.changes || report.WriteAuthorized {
					t.Fatalf("unsafe native diff: %#v", report)
				}
				for _, secret := range []string{"sensitive", "secret", "instance:aginctus-dev", "groups", "plan"} {
					if strings.Contains(output, secret) {
						t.Fatalf("native diff leaked %q: %s", secret, output)
					}
				}
			}
			if *calls != 2 || *projects != 4 || *reads != 4 {
				t.Fatalf("wrong native diff reads: calls=%d projects=%d inventory=%d", *calls, *projects, *reads)
			}
		})
	}
}

func TestScopedWorkloadApplyNativeDryRunFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		preview native.InstancePlan
		err     error
	}{
		{"create", oneScopedDiff("create", 0), nil},
		{"replace", oneScopedDiff("replace", 0), nil},
		{"delete", oneScopedDiff("delete", 0), nil},
		{"unknown", oneScopedDiff("invented", 0), nil},
		{"missing identity", native.InstancePlan{}, nil},
		{"wrong target", native.InstancePlan{Identity: native.InstanceIdentity{Project: "aginctus", Name: "other"}, Action: native.Update}, nil},
		{"wrong project", native.InstancePlan{Identity: native.InstanceIdentity{Project: "foreign", Name: "aginctus-dev"}, Action: native.Update}, nil},
		{"inconsistent unchanged", oneScopedDiff("unchanged", 1), nil},
		{"claims write authority", native.InstancePlan{Identity: native.InstanceIdentity{Project: "aginctus", Name: "aginctus-dev"}, Action: native.Update, WriteAuthorized: true}, nil},
		{"native error", oneScopedDiff("update", 1), errors.New("sensitive Incus credentials")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loader, _, _ := scopedApplyRenderFixture(t)
			calls := stubScopedNativeDiff(t, tc.preview, tc.err)
			code, output, stderr := renderScopedApply(t, loader, "workload", "apply", "--project", "aginctus", "dev", "--dry-run")
			if code != 1 || output != "" || *calls != 1 || stderr == "" || strings.Contains(stderr, "sensitive") {
				t.Fatalf("unsafe native preview %q: code=%d out=%q err=%q calls=%d", tc.name, code, output, stderr, *calls)
			}
		})
	}
}

func TestScopedWorkloadApplyNativeDryRunRejectsDriftAndUnownedProject(t *testing.T) {
	for _, mode := range []string{"foreign", "absent", "changed before second read", "project changed", "invalid grammar"} {
		t.Run(mode, func(t *testing.T) {
			loader, projects, reads := scopedApplyRenderFixture(t)
			calls := stubScopedNativeDiff(t, oneScopedDiff("update", 1), nil)
			switch mode {
			case "foreign":
				inspectScopedInstances = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
					*reads++
					return []byte(`[{"name":"aginctus-dev","status":"Running","config":{}}]`), nil
				}
			case "absent":
				inspectScopedInstances = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
					*reads++
					return []byte("[]"), nil
				}
			case "changed before second read":
				inspectScopedInstances = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
					*reads++
					status := "Running"
					if *reads >= 2 {
						status = "Stopped"
					}
					return []byte("[" + strings.Replace(scopedStatusWorkload, "Running", status, 1) + "]"), nil
				}
			case "project changed":
				inspectLocalProjects = func(context.Context) ([]byte, error) {
					*projects++
					if *projects >= 2 {
						return []byte("[]"), nil
					}
					return []byte(scopedStatusOwnedProject), nil
				}
			}
			args := []string{"workload", "apply", "--project", "aginctus", "dev", "--dry-run"}
			if mode == "invalid grammar" {
				args = append(args, "--force")
			}
			code, out, stderr := renderScopedApply(t, loader, args...)
			if code == 0 || out != "" || stderr == "" {
				t.Fatalf("%s accepted: code=%d out=%q err=%q", mode, code, out, stderr)
			}
			if mode == "invalid grammar" && (*calls != 0 || *projects != 0 || *reads != 0) {
				t.Fatalf("invalid grammar touched environment: calls=%d projects=%d reads=%d", *calls, *projects, *reads)
			}
		})
	}
}

func TestScopedWorkloadApplyNativeDryRunHelpOffline(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"help", "workload"}, {"workload", "--help"}} {
		var output, diagnostic strings.Builder
		code := Run(context.Background(), args, &output, &diagnostic, nil, nil)
		if code != 0 || diagnostic.Len() != 0 ||
			!strings.Contains(output.String(), "workload apply --project NAME WORKLOAD-ID --dry-run") {
			t.Fatalf("missing native dry-run offline help: args=%q code=%d err=%q", args, code, diagnostic.String())
		}
	}
}
