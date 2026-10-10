package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
)

func TestAggregateStatusReadOnlyAndComplete(t *testing.T) {
	oldNetworks, oldProjects, oldGatewayInstances, oldInstances := inspectIncusNetworks, inspectGatewayProjects, inspectGatewayInstances, inspectIncusInstances
	t.Cleanup(func() {
		inspectIncusNetworks = oldNetworks
		inspectGatewayProjects = oldProjects
		inspectGatewayInstances = oldGatewayInstances
		inspectIncusInstances = oldInstances
	})
	inspectIncusNetworks = func(context.Context) ([]byte, error) { return []byte(`[]`), nil }
	inspectGatewayProjects = func(context.Context) ([]byte, error) { return []byte(`[]`), nil }
	inspectGatewayInstances = func(context.Context, string) ([]byte, error) {
		t.Fatal("absent gateway must not query instances")
		return nil, nil
	}
	inspectIncusInstances = func(context.Context) ([]byte, error) { return []byte(`[]`), nil }
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"status"}, &out, &errOut, &fakeIncusClient{}, loader)
	if code != 0 {
		t.Fatalf("code=%d error=%q", code, errOut.String())
	}
	for _, want := range []string{"management network", "gateway", "Herdr client", "workload"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in %q", want, out.String())
		}
	}
}

func TestAggregateStatusDoesNotPrintPartialResults(t *testing.T) {
	oldNetworks, oldProjects := inspectIncusNetworks, inspectGatewayProjects
	t.Cleanup(func() { inspectIncusNetworks = oldNetworks; inspectGatewayProjects = oldProjects })
	inspectIncusNetworks = func(context.Context) ([]byte, error) { return []byte(`[]`), nil }
	inspectGatewayProjects = func(context.Context) ([]byte, error) { return nil, errors.New("offline") }
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"status"}, &out, &errOut, &fakeIncusClient{}, loader)
	if code != 1 || out.Len() != 0 || !strings.Contains(errOut.String(), "offline") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}

func TestAggregateStatusRejectsArguments(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runAggregateStatus(context.Background(), []string{"--force"}, &out, &errOut, nil)
	if code != 2 || !strings.Contains(errOut.String(), "Usage:") {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
}
