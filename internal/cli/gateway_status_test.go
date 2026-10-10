package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
)

func TestGatewayStatus(t *testing.T) {
	oldProjects, oldInstances := inspectGatewayProjects, inspectGatewayInstances
	t.Cleanup(func() { inspectGatewayProjects = oldProjects; inspectGatewayInstances = oldInstances })
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	cases := []struct {
		name, projects, instances, want string
		fail                            bool
	}{
		{"absent", `[]`, "", "absent", false},
		{"healthy", `[{"name":"aginctus-gateway","config":{"user.aginctus.managed":"true","user.aginctus.resource":"compose-project","user.aginctus.deployment":"aginctus-gateway"}}]`, `[{"name":"postgres-1","status":"Running","config":{"user.aginctus.managed":"true","user.aginctus.resource":"compose-instance","user.aginctus.deployment":"aginctus-gateway"}},{"name":"litellm-1","status":"Stopped","config":{"user.aginctus.managed":"true","user.aginctus.resource":"compose-instance","user.aginctus.deployment":"aginctus-gateway"}}]`, "1/2", false},
		{"retained empty project", `[{"name":"aginctus-gateway","config":{"user.aginctus.managed":"true","user.aginctus.resource":"compose-project","user.aginctus.deployment":"aginctus-gateway"}}]`, `[]`, "inactive (no service instances; persistent data retained)", false},
		{"all services stopped", `[{"name":"aginctus-gateway","config":{"user.aginctus.managed":"true","user.aginctus.resource":"compose-project","user.aginctus.deployment":"aginctus-gateway"}}]`, `[{"name":"postgres-1","status":"Stopped","config":{"user.aginctus.managed":"true","user.aginctus.resource":"compose-instance","user.aginctus.deployment":"aginctus-gateway"}}]`, "0/1 service instances running", false},
		{"foreign project", `[{"name":"aginctus-gateway","config":{}}]`, "", "ownership", true},
		{"foreign service", `[{"name":"aginctus-gateway","config":{"user.aginctus.managed":"true","user.aginctus.resource":"compose-project","user.aginctus.deployment":"aginctus-gateway"}}]`, `[{"name":"unrelated","config":{}}]`, "unexpected", true},
		{"invalid projects", `{`, "", "decode", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inspectGatewayProjects = func(context.Context) ([]byte, error) { return []byte(tc.projects), nil }
			inspectGatewayInstances = func(context.Context, string) ([]byte, error) { return []byte(tc.instances), nil }
			var out, errOut bytes.Buffer
			code := Run(context.Background(), []string{"--config=gateway.compose.project=aginctus-gateway", "gateway", "status"}, &out, &errOut, &fakeIncusClient{}, loader)
			wantCode := 0
			if tc.fail {
				wantCode = 1
			}
			if code != wantCode || !strings.Contains(out.String()+errOut.String(), tc.want) {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
		})
	}
}

func TestGatewayStatusDaemonFailure(t *testing.T) {
	old := inspectGatewayProjects
	t.Cleanup(func() { inspectGatewayProjects = old })
	inspectGatewayProjects = func(context.Context) ([]byte, error) { return nil, errors.New("offline") }
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"gateway", "status"}, &out, &errOut, &fakeIncusClient{}, loader)
	if code != 1 || !strings.Contains(errOut.String(), "offline") {
		t.Fatalf("code=%d error=%q", code, errOut.String())
	}
}
