package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
)

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

func TestWorkloadListRejectsForeignAndDaemonErrors(t *testing.T) {
	previous := inspectIncusInstances
	t.Cleanup(func() { inspectIncusInstances = previous })
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	for _, tc := range []struct {
		name string
		raw  string
		err  error
		want string
	}{
		{"foreign", `[{"name":"aginctus-dev","status":"Running","config":{}}]`, nil, "ownership"},
		{"daemon", "", errors.New("unavailable"), "unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inspectIncusInstances = func(context.Context) ([]byte, error) { return []byte(tc.raw), tc.err }
			var out, errOut bytes.Buffer
			code := Run(context.Background(), []string{"workload", "ls"}, &out, &errOut, &fakeIncusClient{}, loader)
			if code != 1 || out.Len() != 0 || !strings.Contains(errOut.String(), tc.want) {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
		})
	}
}
