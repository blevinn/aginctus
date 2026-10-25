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

const scopedStatusOwnedProject = `[{"name":"aginctus","config":{"user.aginctus.managed":"true","user.aginctus.resource":"deployment-project","user.aginctus.deployment":"aginctus","features.images":"true","features.profiles":"true","features.storage.volumes":"true","features.networks":"false"}}]`
const scopedStatusWorkload = `{"name":"aginctus-dev","status":"Running","config":{"user.aginctus.managed":"true","user.aginctus.resource":"workload","user.aginctus.role":"agent-workload","user.aginctus.workload":"dev"},"project":"aginctus"}`
const scopedStatusHerdr = `{"name":"aginctus-herdr","status":"Stopped","config":{"user.aginctus.managed":"true","user.aginctus.resource":"infrastructure","user.aginctus.role":"herdr-client"},"project":"aginctus"}`

func TestScopedInstanceListArgumentsAndParsing(t *testing.T) {
	scope, _ := incusscope.NewProjectScope("aginctus")
	want := []string{"--force-local", "--project", "aginctus", "list", "--format", "json"}
	if got := scopedInstanceListArgs(scope); !reflect.DeepEqual(got, want) {
		t.Fatalf("scoped incus argv=%q want=%q", got, want)
	}
	for _, args := range [][]string{
		{"--project", "aginctus"}, {"--project=aginctus"},
	} {
		name, rest, err := parseScopedInstanceStatusArgs(args)
		if err != nil || name != "aginctus" || len(rest) != 0 {
			t.Fatalf("valid project args %q got %q %q err=%v", args, name, rest, err)
		}
	}
	for _, args := range [][]string{
		nil, {"--project"}, {"--project", "default"},
		{"--project=../escape"}, {"--project="},
		{"dev", "--project", "aginctus"},
	} {
		if _, _, err := parseScopedInstanceStatusArgs(args); err == nil {
			t.Errorf("accepted invalid project %q", args)
		}
	}
}

func TestScopedInstanceStatusCommands(t *testing.T) {
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	savedProjects, savedScoped, savedLegacy := inspectLocalProjects, inspectScopedInstances, inspectIncusInstances
	t.Cleanup(func() {
		inspectLocalProjects, inspectScopedInstances, inspectIncusInstances = savedProjects, savedScoped, savedLegacy
	})
	inspectLocalProjects = func(context.Context) ([]byte, error) {
		return []byte(scopedStatusOwnedProject), nil
	}
	inspectIncusInstances = func(context.Context) ([]byte, error) {
		t.Fatal("opt-in scoped instance status queried v0.1 all-projects listing")
		return nil, nil
	}
	inspectScopedInstances = func(_ context.Context, scope incusscope.ProjectScope) ([]byte, error) {
		if scope.Name != "aginctus" {
			t.Fatalf("unexpected scoped inventory %q", scope.Name)
		}
		return []byte("[" + scopedStatusWorkload + "," + scopedStatusHerdr + "]"), nil
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"workload", "status", "--project", "aginctus", "dev"}, `workload "dev": Running`},
		{[]string{"workload", "status", "--project=aginctus"}, `workload "dev": Running`},
		{[]string{"workload", "ls", "--project", "aginctus"}, "dev"},
		{[]string{"workload", "list", "--project=aginctus"}, "dev"},
		{[]string{"herdr", "client", "status", "--project", "aginctus"}, `Herdr client "aginctus-herdr": Stopped`},
	} {
		var out, errOut bytes.Buffer
		code := Run(context.Background(), tc.args, &out, &errOut, &fakeIncusClient{}, loader)
		if code != 0 || strings.TrimSpace(out.String()) != tc.want || errOut.Len() != 0 {
			t.Fatalf("scoped status args=%q code=%d stdout=%q stderr=%q", tc.args, code, out.String(), errOut.String())
		}
	}
}

func TestScopedInstanceStatusFailsClosed(t *testing.T) {
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	beforeProjects, beforeScoped, beforeLegacy := inspectLocalProjects, inspectScopedInstances, inspectIncusInstances
	t.Cleanup(func() {
		inspectLocalProjects, inspectScopedInstances, inspectIncusInstances = beforeProjects, beforeScoped, beforeLegacy
	})
	inspectIncusInstances = func(context.Context) ([]byte, error) {
		t.Fatal("scoped instance status must not use ambient project")
		return nil, nil
	}
	project := scopedStatusOwnedProject
	data := "[" + scopedStatusWorkload + "]"
	var readError error
	reads := 0
	inspectLocalProjects = func(context.Context) ([]byte, error) {
		if project == "error" {
			return nil, errors.New("sensitive external project output")
		}
		return []byte(project), nil
	}
	inspectScopedInstances = func(_ context.Context, scope incusscope.ProjectScope) ([]byte, error) {
		reads++
		if scope.Name != "aginctus" {
			t.Fatalf("unapproved project accessed: %s", scope.Name)
		}
		return []byte(data), readError
	}
	cases := []struct {
		name, projects, instances string
		readErr                   bool
		expectReads               int
	}{
		{"wrong requested project", scopedStatusOwnedProject, data, false, 0},
		{"foreign project", strings.Replace(scopedStatusOwnedProject, "deployment-project", "not-owned", 1), data, false, 0},
		{"unavailable project", "error", data, false, 0},
		{"missing project", "[]", data, false, 0},
		{"foreign same-name workload", scopedStatusOwnedProject, `[{"name":"aginctus-dev","config":{},"project":"aginctus"}]`, false, 1},
		{"wrong response project", scopedStatusOwnedProject, "[" + strings.Replace(scopedStatusWorkload, `"project":"aginctus"`, `"project":"default"`, 1) + "]", false, 1},
		{"duplicate instance names", scopedStatusOwnedProject, "[" + scopedStatusWorkload + "," + scopedStatusWorkload + "]", false, 1},
		{"invalid instance JSON", scopedStatusOwnedProject, "null", false, 1},
		{"oversized instance list", scopedStatusOwnedProject, strings.Repeat("x", maxProjectListBytes+1), false, 1},
		{"inventory denied", scopedStatusOwnedProject, data, true, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			project, data, readError, reads = tc.projects, tc.instances, nil, 0
			if tc.readErr {
				readError = errors.New("sensitive external instance output")
			}
			args := []string{"workload", "status", "--project", "aginctus", "dev"}
			if tc.name == "wrong requested project" {
				args[3] = "other"
			}
			var out, errOut bytes.Buffer
			code := Run(context.Background(), args, &out, &errOut, &fakeIncusClient{}, loader)
			if code == 0 || out.Len() != 0 || reads != tc.expectReads {
				t.Fatalf("code=%d stdout=%q stderr=%q reads=%d expected reads=%d", code, out.String(), errOut.String(), reads, tc.expectReads)
			}
			if strings.Contains(errOut.String(), "sensitive") {
				t.Fatal("leaked raw subprocess output")
			}
		})
	}
}

func TestScopedWorkloadListNeverPrintsPartialResults(t *testing.T) {
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	oldProjects, oldInstances := inspectLocalProjects, inspectScopedInstances
	t.Cleanup(func() {
		inspectLocalProjects, inspectScopedInstances = oldProjects, oldInstances
	})
	inspectLocalProjects = func(context.Context) ([]byte, error) { return []byte(scopedStatusOwnedProject), nil }
	// Set two configured workloads, then make the second an ownership collision.
	inspectScopedInstances = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
		return []byte("[" + scopedStatusWorkload + `,{"name":"aginctus-other","status":"Running","config":{}}` + "]"), nil
	}
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{
		"--config=workloads.other.instance.name=aginctus-other",
		"--config=workloads.other.runtime.type=opencode",
		"--config=workloads.other.isolation=container",
		"--config=workloads.other.image.alias=aginctus-opencode-workload",
		"workload", "ls", "--project", "aginctus",
	}, &out, &errOut, &fakeIncusClient{}, loader)
	if code != 1 || out.Len() != 0 {
		t.Fatalf("partial listing not suppressed: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}
