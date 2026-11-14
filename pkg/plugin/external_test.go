package plugin_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The fixture is a separate module outside Aginctus's import namespace: Go must
// reject any accidental reliance on this repository's internal packages. Root
// go test ./... does not discover nested modules, so run it explicitly here.
func TestExternalModule(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, "go", args...)
		cmd.Dir = "testdata/external"
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go %v: %v\n%s", args, err, output)
		}
		return string(output)
	}
	run("test", "-count=1", "./...")
	// An indirect internal dependency would still compile, so check the public
	// dependency closure too, not just the fixture's direct imports.
	deps := run("list", "-deps", "-f", "{{.ImportPath}}", "github.com/blevinn/aginctus/pkg/plugin")
	for _, dependency := range strings.Fields(deps) {
		if strings.HasPrefix(dependency, "github.com/blevinn/aginctus/internal/") ||
			strings.HasPrefix(dependency, "github.com/blevinn/aginctus/plugins/") {
			t.Fatalf("public contract depends on implementation package %s", dependency)
		}
	}
}
