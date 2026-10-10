package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
)

func TestFindManagedInstance(t *testing.T) {
	cases := []struct {
		name      string
		instances []inspectedInstance
		want      string
		fail      bool
	}{
		{"absent", nil, "", false},
		{"owned", []inspectedInstance{{Name: "aginctus-dev", Status: "Running", Config: map[string]string{"user.aginctus.managed": "true", "user.aginctus.resource": "workload", "user.aginctus.role": "agent-workload", "user.aginctus.workload": "dev"}}}, "Running", false},
		{"foreign", []inspectedInstance{{Name: "aginctus-dev", Status: "Running"}}, "", true},
		{"wrong id", []inspectedInstance{{Name: "aginctus-dev", Config: map[string]string{"user.aginctus.managed": "true", "user.aginctus.resource": "workload", "user.aginctus.role": "agent-workload", "user.aginctus.workload": "other"}}}, "", true},
		{"wrong project", []inspectedInstance{{Name: "aginctus-dev", Project: "guest", Config: map[string]string{"user.aginctus.managed": "true", "user.aginctus.resource": "workload", "user.aginctus.role": "agent-workload", "user.aginctus.workload": "dev"}}}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := findManagedInstance(tc.instances, "aginctus-dev", "workload", "agent-workload", "dev")
			if (err != nil) != tc.fail || got != tc.want {
				t.Fatalf("status=%q error=%v", got, err)
			}
		})
	}
}

func TestWorkloadStatusUsesLiveIncusData(t *testing.T) {
	previous := inspectIncusInstances
	t.Cleanup(func() { inspectIncusInstances = previous })
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	for _, tc := range []struct {
		name, raw, want string
		code            int
	}{
		{"absent", `[]`, "workload \"dev\": absent", 0},
		{"owned", `[{"name":"aginctus-dev","status":"Running","config":{"user.aginctus.managed":"true","user.aginctus.resource":"workload","user.aginctus.role":"agent-workload","user.aginctus.workload":"dev"}}]`, "Running", 0},
		{"foreign", `[{"name":"aginctus-dev","status":"Running","config":{}}]`, "ownership", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inspectIncusInstances = func(context.Context) ([]byte, error) { return []byte(tc.raw), nil }
			var out, errOut bytes.Buffer
			code := Run(context.Background(), []string{"--config=workloads.dev.instance.name=aginctus-dev", "workload", "status", "dev"}, &out, &errOut, &fakeIncusClient{}, loader)
			if code != tc.code || !strings.Contains(out.String()+errOut.String(), tc.want) {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
		})
	}
}

func TestHerdrClientStatusRejectsDaemonError(t *testing.T) {
	previous := inspectIncusInstances
	t.Cleanup(func() { inspectIncusInstances = previous })
	inspectIncusInstances = func(context.Context) ([]byte, error) { return nil, errors.New("offline") }
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"herdr", "client", "status"}, &out, &errOut, &fakeIncusClient{}, loader)
	if code != 1 || !strings.Contains(errOut.String(), "offline") {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
}
