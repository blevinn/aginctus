package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
)

func TestHerdrConsoleRequiresOwnedRunningClient(t *testing.T) {
	oldInspect, oldLaunch := inspectIncusInstances, launchHerdrConsole
	t.Cleanup(func() { inspectIncusInstances = oldInspect; launchHerdrConsole = oldLaunch })
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	called := 0
	launchHerdrConsole = func(context.Context, string, io.Reader, io.Writer, io.Writer) error { called++; return nil }
	cases := []struct {
		name, data string
		code       int
	}{
		{"absent", `[]`, 1},
		{"stopped", `[{"name":"aginctus-herdr","status":"Stopped","config":{"user.aginctus.managed":"true","user.aginctus.resource":"infrastructure","user.aginctus.role":"herdr-client"}}]`, 1},
		{"unowned", `[{"name":"aginctus-herdr","status":"Running","config":{}}]`, 1},
		{"running", `[{"name":"aginctus-herdr","status":"Running","config":{"user.aginctus.managed":"true","user.aginctus.resource":"infrastructure","user.aginctus.role":"herdr-client"}}]`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inspectIncusInstances = func(context.Context) ([]byte, error) { return []byte(tc.data), nil }
			var out, stderr bytes.Buffer
			got := Run(context.Background(), []string{"herdr"}, &out, &stderr, &fakeIncusClient{}, loader)
			if got != tc.code {
				t.Fatalf("exit=%d stderr=%q", got, stderr.String())
			}
		})
	}
	if called != 1 {
		t.Fatalf("launches=%d, want 1", called)
	}
}
func TestHerdrConsolePropagatesExitFailure(t *testing.T) {
	oldInspect, oldLaunch := inspectIncusInstances, launchHerdrConsole
	t.Cleanup(func() { inspectIncusInstances = oldInspect; launchHerdrConsole = oldLaunch })
	inspectIncusInstances = func(context.Context) ([]byte, error) {
		return []byte(`[{"name":"aginctus-herdr","status":"Running","config":{"user.aginctus.managed":"true","user.aginctus.resource":"infrastructure","user.aginctus.role":"herdr-client"}}]`), nil
	}
	launchHerdrConsole = func(context.Context, string, io.Reader, io.Writer, io.Writer) error {
		return errors.New("console exited")
	}
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	var out, stderr bytes.Buffer
	got := Run(context.Background(), []string{"herdr"}, &out, &stderr, &fakeIncusClient{}, loader)
	if got != 1 || !strings.Contains(stderr.String(), "console exited") {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
}
