package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/pkg/api"
)

func TestScopedWorkloadTypedOutputAndLegacyDefault(t *testing.T) {
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil

	savedProjects := inspectLocalProjects
	savedScoped := inspectScopedInstances
	savedLegacy := inspectIncusInstances
	t.Cleanup(func() {
		inspectLocalProjects = savedProjects
		inspectScopedInstances = savedScoped
		inspectIncusInstances = savedLegacy
	})
	inspectLocalProjects = func(context.Context) ([]byte, error) {
		return []byte(scopedStatusOwnedProject), nil
	}
	inspectScopedInstances = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
		return []byte("[" + scopedStatusWorkload + "]"), nil
	}
	inspectIncusInstances = func(context.Context) ([]byte, error) {
		t.Fatal("scoped typed read used all-projects legacy inventory")
		return nil, nil
	}

	tests := []struct {
		args []string
		want string
	}{
		{[]string{"workload", "ls", "--project", "aginctus"}, "dev\n"},
		{[]string{"workload", "ls", "--project", "aginctus", "-o", "name"}, "dev\n"},
		{[]string{"workload", "status", "--project", "aginctus", "dev"}, "workload \"dev\": Running\n"},
		{[]string{"workload", "ls", "--project", "aginctus", "-o", "table"}, "NAME"},
		{[]string{"workload", "status", "--project", "aginctus", "-o", "wide"}, "INSTANCE"},
	}
	for _, tc := range tests {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), tc.args, &stdout, &stderr, &fakeIncusClient{}, loader)
			if code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), tc.want) {
				t.Fatalf("code=%d stdout=%q stderr=%q want=%q", code, stdout.String(), stderr.String(), tc.want)
			}
		})
	}

	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"-o", "json", "workload", "status", "--project=aginctus", "dev"}, &stdout, &stderr, &fakeIncusClient{}, loader); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	var list api.ScopedWorkloadStatusList
	if err := json.Unmarshal(stdout.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.APIVersion != api.APIVersionV1Alpha1 || list.Kind != "ScopedWorkloadStatusList" || list.Project != "aginctus" ||
		len(list.Items) != 1 || list.Items[0].Name != "dev" || list.Items[0].State != "Running" {
		t.Fatalf("incorrect typed workload output: %#v", list)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"workload", "list", "-o", "yaml", "--project", "aginctus"}, &stdout, &stderr, &fakeIncusClient{}, loader); code != 0 || !strings.Contains(stdout.String(), "kind: ScopedWorkloadStatusList") {
		t.Fatalf("YAML code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestScopedWorkloadTypedOutputFailsClosed(t *testing.T) {
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	savedProjects := inspectLocalProjects
	savedScoped := inspectScopedInstances
	t.Cleanup(func() {
		inspectLocalProjects = savedProjects
		inspectScopedInstances = savedScoped
	})
	inspectLocalProjects = func(context.Context) ([]byte, error) {
		return []byte(scopedStatusOwnedProject), nil
	}
	inspectScopedInstances = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
		return []byte(`[{"name":"aginctus-dev","status":"Running","config":{"user.aginctus.resource":"foreign"},"project":"aginctus"}]`), nil
	}
	for _, args := range [][]string{
		{"-o", "json", "workload", "ls"},
		{"-o", "json", "workload", "status", "--project", "other", "dev"},
		{"-o", "json", "workload", "ls", "--project", "aginctus"},
	} {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), args, &stdout, &stderr, &fakeIncusClient{}, loader)
		if code == 0 || stdout.Len() != 0 {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}
}
