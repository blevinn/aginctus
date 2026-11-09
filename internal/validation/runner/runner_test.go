package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/validation/catalog"
)

func TestSanitizeEnv(t *testing.T) {
	safe := strings.Join(SanitizeEnv([]string{
		"PATH=/bin", "GOFLAGS=-run=^$", "AGINCTUS_ACCEPTANCE_LIVE=YES",
		"AGINCTUS_INCUS_DISPOSABLE=YES", "AGINCTUS_ACCEPTANCE_LIVE_HOOKS=/tmp/key",
		"GOPROXY=https://outside", "NORMAL=keep",
	}), "\n")
	for _, expected := range []string{
		"GOFLAGS=", "AGINCTUS_ACCEPTANCE_LIVE=", "AGINCTUS_ACCEPTANCE_LIVE_HOOKS=",
		"AGINCTUS_INCUS_INTEGRATION=0", "AGINCTUS_INCUS_DISPOSABLE=NO",
		"GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "NORMAL=keep",
	} {
		if !strings.Contains(safe, expected+"\n") && !strings.HasSuffix(safe, expected) {
			t.Fatalf("missing forced setting %q", expected)
		}
	}
}

func TestCollectorBoundsAndFailureCases(t *testing.T) {
	c := newFailureCollector("go")
	payload := "{\"Action\":\"fail\",\"Package\":\"github.com/blevinn/aginctus/internal/test\",\"Test\":\"TestFail\"}\n"
	_, _ = c.Write([]byte(payload + strings.Repeat("SENSITIVE", 1000) + "\n"))
	for i := range 9 {
		_, _ = fmt.Fprintf(c, "{\"Action\":\"fail\",\"Package\":\"github.com/blevinn/aginctus/internal/test\",\"Test\":\"TestFail%d\"}\n", i)
	}
	if len(c.cases) != maxCases || !c.truncated {
		t.Fatalf("want bounded cases and truncation, got %#v, truncated=%v", c.cases, c.truncated)
	}
	if strings.Contains(strings.TrimSpace(string(c.line)), "SENSITIVE") {
		t.Fatal("raw output retained")
	}
	p := newFailureCollector("python")
	_, _ = p.Write([]byte("ERROR: test_bad (test_module.Checks)\n"))
	if len(p.cases) != 1 || p.cases[0].ID != "test_module.Checks" || p.cases[0].Kind != "error" {
		t.Fatalf("wrong unittest error parsing: %#v", p.cases)
	}
}

func TestRunFixedHermeticSuitesAndRedaction(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "just")
	// No real suite runs. Check inherited privilege flags before emitting
	// failure identifiers plus a raw secret canary.
	script := `#!/bin/sh
[ "$1" = check ] || exit 96
[ "$AGINCTUS_INCUS_INTEGRATION" = 0 ] || exit 96
[ "$AGINCTUS_INCUS_DISPOSABLE" = NO ] || exit 96
[ -z "$AGINCTUS_ACCEPTANCE_LIVE" ] || exit 96
[ -z "$AGINCTUS_ACCEPTANCE_LIVE_HOOKS" ] || exit 96
[ -z "$GOFLAGS" ] || exit 96
[ "$GOPROXY" = off ] || exit 96
if [ "$2" = go ]; then
  echo '{"Action":"fail","Package":"github.com/blevinn/aginctus/internal/test","Test":"TestBroken"}'
  echo SECRET_NEVER_IN_REPORT
  echo SECRET_STDERR_NEVER_IN_REPORT >&2
  exit 7
fi
if [ "$2" = python ]; then
  echo 'FAIL: test_bad (module.Checks)'
  exit 5
fi
exit 0
`
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	env := append(os.Environ(), "GOFLAGS=-run=^$",
		"AGINCTUS_ACCEPTANCE_LIVE=YES", "AGINCTUS_ACCEPTANCE_LIVE_HOOKS=/tmp/unsafe",
		"AGINCTUS_INCUS_INTEGRATION=1", "AGINCTUS_INCUS_DISPOSABLE=YES")
	report := Run(context.Background(), dir, env, false, nil)
	if report.SchemaVersion != 1 || report.Profile != "hermetic" || report.Status != "fail" ||
		len(report.Results) != 8 || report.Summary.Passed != 6 || report.Summary.Failed != 2 {
		t.Fatalf("unexpected report summary: %#v", report)
	}
	if report.Results[0].ExitCode == nil || *report.Results[0].ExitCode != 7 ||
		len(report.Results[0].FailedCases) != 1 || report.Results[0].FailedCases[0].ID != "github.com/blevinn/aginctus/internal/test:TestBroken" {
		t.Fatalf("missing Go failure detail: %#v", report.Results[0])
	}
	if report.Results[2].FailedCases[0].ID != "module.Checks" {
		t.Fatalf("missing unittest detail: %#v", report.Results[2])
	}
	var output bytes.Buffer
	if err := WriteJSON(&output, report); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "SECRET_NEVER_IN_REPORT") ||
		strings.Contains(output.String(), "SECRET_STDERR_NEVER_IN_REPORT") ||
		len(bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n"))) != 1 {
		t.Fatal("raw output or extra lines in machine report")
	}
	var parsed map[string]any
	if json.Unmarshal(output.Bytes(), &parsed) != nil {
		t.Fatal("machine report is not JSON")
	}
	output.Reset()
	if err := WriteText(&output, report); err != nil || !strings.Contains(output.String(), "Hermetic: 6 passed, 2 failed, 0 errors") {
		t.Fatalf("text report mismatch: %q, %v", output.String(), err)
	}
}

func TestLaunchErrorsAreDistinct(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	env := []string{"PATH=" + dir}
	report := Run(context.Background(), dir, env, false, nil)
	if report.Summary.Errors != 8 || report.Status != "fail" {
		t.Fatalf("missing fail-closed launch errors: %#v", report.Summary)
	}
	for _, r := range report.Results {
		if r.Status != "error" || r.ExitCode != nil {
			t.Fatalf("incorrect launch result: %#v", r)
		}
	}
}

func TestFailureParsingFragmentsAndUntrustedIdentifiers(t *testing.T) {
	py := newFailureCollector("python")
	_, _ = py.Write([]byte("FAIL: test_one (test_foo.Case."))
	_, _ = py.Write([]byte("test_one)\n"))
	_, _ = py.Write([]byte("FAIL: test_one (test_foo.Case.test_one)\n"))
	_, _ = py.Write([]byte("ERROR: test_two (test_foo.Case.test_two)\n"))
	_, _ = py.Write([]byte("FAIL: test_bad (something;BAD=SECRET)\n"))
	_, _ = py.Write([]byte(strings.Repeat("SECRET_CANARY", maxLineBytes) + "\n"))
	_, _ = py.Write([]byte("FAIL: test_three (test_foo.Case.test_three)\n"))
	want := []FailureCase{
		{ID: "test_foo.Case.test_one", Kind: "fail"},
		{ID: "test_foo.Case.test_two", Kind: "error"},
		{ID: "test_foo.Case.test_three", Kind: "fail"},
	}
	if !reflect.DeepEqual(py.cases, want) || py.truncated {
		t.Fatalf("unittest failure parsing drifted: %#v", py.cases)
	}
	goCases := newFailureCollector("go")
	events := []string{
		"{\"Action\":\"output\",\"Package\":\"github.com/blevinn/aginctus/internal/cli\",\"Test\":\"TestOne\",\"Output\":\"SECRET_CANARY\"}",
		"{\"Action\":\"fail\",\"Package\":\"github.com/blevinn/aginctus/internal/cli\"}",
		"{\"Action\":\"fail\",\"Package\":\"github.com/blevinn/aginctus/internal/cli\",\"Test\":\"TestOne\"}",
		"{\"Action\":\"fail\",\"Package\":\"github.com/other/project\",\"Test\":\"TestTwo\"}",
		"{\"Action\":\"fail\",\"Package\":\"github.com/blevinn/aginctusbad\",\"Test\":\"TestThree\"}",
	}
	for _, line := range events {
		_, _ = goCases.Write([]byte(line + "\n"))
	}
	if len(goCases.cases) != 1 || goCases.cases[0].ID != "github.com/blevinn/aginctus/internal/cli:TestOne" {
		t.Fatalf("untrusted Go event was accepted: %#v", goCases.cases)
	}
}

func TestCanceledSuiteIsAnErrorNotAFailure(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "just")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := runSuite(ctx, dir, catalog.HermeticSuites()[0], SanitizeEnv([]string{"PATH=" + dir}), false, nil)
	if result.Status != "error" || result.ExitCode != nil {
		t.Fatalf("canceled suite was not reported as an error: %#v", result)
	}
}

func TestCatalogTimeoutIsBoundedAndNotCallerControlled(t *testing.T) {
	suites := catalog.HermeticSuites()
	for _, suite := range suites {
		if suite.TimeoutSeconds <= 0 || suite.TimeoutSeconds > 3600 || suite.Authority != catalog.DeveloperCI {
			t.Fatalf("unsafe hermetic execution metadata: %#v", suite)
		}
	}
	bad := suites[0]
	bad.TimeoutSeconds = 0
	result := runSuite(context.Background(), t.TempDir(), bad, nil, false, nil)
	if result.Status != "error" || result.ExitCode != nil {
		t.Fatalf("unbounded catalog timeout was accepted: %#v", result)
	}
}
