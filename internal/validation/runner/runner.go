// Package runner implements the hermetic reporting boundary. It cannot
// select VM or live checks and never retains raw subprocess output.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/blevinn/aginctus/internal/validation/catalog"
)

const (
	maxLineBytes = 4096
	maxCases     = 8
)

var (
	caseID        = regexp.MustCompile("^[A-Za-z0-9_./:-]{1,192}$")
	pythonFailure = regexp.MustCompile(`^(FAIL|ERROR): [A-Za-z_][A-Za-z0-9_]* \(([A-Za-z_][A-Za-z0-9_.]{1,190})\)$`)
)

// FailureCase contains a bounded, allowlisted identifier, never test output.
type FailureCase struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

type Result struct {
	ID             string        `json:"id"`
	Status         string        `json:"status"`
	ExitCode       *int          `json:"exit_code"`
	DurationMS     int64         `json:"duration_ms"`
	Rerun          string        `json:"rerun"`
	FailedCases    []FailureCase `json:"failed_cases,omitempty"`
	CasesTruncated bool          `json:"cases_truncated,omitempty"`
}

type Summary struct {
	Passed int `json:"passed"`
	Failed int `json:"failed"`
	Errors int `json:"errors"`
}

type Report struct {
	SchemaVersion int      `json:"schema_version"`
	Profile       string   `json:"profile"`
	Status        string   `json:"status"`
	DurationMS    int64    `json:"duration_ms"`
	Summary       Summary  `json:"summary"`
	Results       []Result `json:"results"`
}

// SanitizeEnv overwrites inherited integration opt-ins for every child.
// The caller's PATH is retained to find pinned development-shell tools.
func SanitizeEnv(in []string) []string {
	values := make(map[string]string, len(in))
	for _, item := range in {
		if index := strings.IndexByte(item, '='); index > 0 {
			values[item[:index]] = item[index+1:]
		}
	}
	for key, value := range map[string]string{
		"AGINCTUS_INCUS_INTEGRATION":     "0",
		"AGINCTUS_INCUS_DISPOSABLE":      "NO",
		"AGINCTUS_ACCEPTANCE_LIVE":       "",
		"AGINCTUS_ACCEPTANCE_LIVE_HOOKS": "",
		"ACCEPTANCE_TEST_FAIL":           "",
		"GOFLAGS":                        "",
		"GOPROXY":                        "off",
		"GOSUMDB":                        "off",
		"GOTOOLCHAIN":                    "local",
	} {
		values[key] = value
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	slices.Sort(names)
	result := make([]string, 0, len(names))
	for _, name := range names {
		result = append(result, name+"="+values[name])
	}
	return result
}

func withStructuredFlag(env []string, on bool) []string {
	value := "AGINCTUS_CHECK_STRUCTURED=0"
	if on {
		value = "AGINCTUS_CHECK_STRUCTURED=1"
	}
	return append(append([]string(nil), env...), value)
}

type failureCollector struct {
	suite     string
	line      []byte
	discard   bool
	cases     []FailureCase
	truncated bool
	seen      map[string]bool
}

func newFailureCollector(suite string) *failureCollector {
	return &failureCollector{suite: suite, seen: make(map[string]bool)}
}

// Write processes bounded lines; even a malicious child emitting unbounded
// output cannot make the collector retain full lines or copy logs to JSON.
func (c *failureCollector) Write(data []byte) (int, error) {
	for _, b := range data {
		if b == '\n' {
			if !c.discard {
				c.parse(strings.TrimSuffix(string(c.line), "\r"))
			}
			c.line = c.line[:0]
			c.discard = false
		} else if !c.discard {
			if len(c.line) >= maxLineBytes {
				c.discard = true
				c.line = c.line[:0]
			} else {
				c.line = append(c.line, b)
			}
		}
	}
	return len(data), nil
}

func (c *failureCollector) add(id, kind string) {
	if !caseID.MatchString(id) || c.seen[id] {
		return
	}
	c.seen[id] = true
	if len(c.cases) >= maxCases {
		c.truncated = true
		return
	}
	c.cases = append(c.cases, FailureCase{ID: id, Kind: kind})
}

func (c *failureCollector) parse(line string) {
	switch c.suite {
	case "python":
		match := pythonFailure.FindStringSubmatch(line)
		if match != nil {
			kind := "fail"
			if match[1] == "ERROR" {
				kind = "error"
			}
			c.add(match[2], kind)
		}
	case "go":
		if !strings.HasPrefix(line, "{") {
			return
		}
		var event struct {
			Action  string
			Package string
			Test    string
		}
		if json.Unmarshal([]byte(line), &event) != nil || event.Action != "fail" {
			return
		}
		if caseID.MatchString(event.Test) && (event.Package == "github.com/blevinn/aginctus" ||
			strings.HasPrefix(event.Package, "github.com/blevinn/aginctus/")) {
			c.add(event.Package+":"+event.Test, "fail")
		}
	}
}

func runSuite(ctx context.Context, root string, suite catalog.Suite, safeEnv []string, verbose bool, diagnostic io.Writer) Result {
	start := time.Now()
	rerun := "just check " + suite.Recipe
	cmd := exec.CommandContext(ctx, "just", "check", suite.Recipe)
	cmd.Dir = root
	cmd.Env = withStructuredFlag(safeEnv, suite.ID == "go")
	cmd.Stdin = nil
	collector := newFailureCollector(suite.ID)
	if verbose {
		cmd.Stdout, cmd.Stderr = diagnostic, diagnostic
	} else {
		cmd.Stdout, cmd.Stderr = collector, collector
	}
	err := cmd.Run()
	result := Result{ID: suite.ID, Rerun: rerun, DurationMS: time.Since(start).Milliseconds()}
	if err == nil {
		zero := 0
		result.ExitCode, result.Status = &zero, "pass"
	} else {
		var exit *exec.ExitError
		if errors.As(err, &exit) && ctx.Err() == nil {
			code := exit.ExitCode()
			result.ExitCode, result.Status = &code, "fail"
		} else {
			result.Status = "error"
		}
	}
	if !verbose && result.Status == "fail" && len(collector.cases) != 0 {
		result.FailedCases = collector.cases
		result.CasesTruncated = collector.truncated
	}
	return result
}

// Run invokes only the fixed hermetic catalog. Failure of one suite does not
// prevent subsequent independent suite reporting, as in the existing v1 runner.
func Run(ctx context.Context, root string, inheritedEnv []string, verbose bool, diagnostic io.Writer) Report {
	if diagnostic == nil {
		diagnostic = io.Discard
	}
	start := time.Now()
	suites := catalog.HermeticSuites()
	report := Report{SchemaVersion: 1, Profile: "hermetic", Results: make([]Result, 0, len(suites))}
	env := SanitizeEnv(inheritedEnv)
	for _, suite := range suites {
		result := runSuite(ctx, root, suite, env, verbose, diagnostic)
		report.Results = append(report.Results, result)
		switch result.Status {
		case "pass":
			report.Summary.Passed++
		case "fail":
			report.Summary.Failed++
		default:
			report.Summary.Errors++
		}
	}
	report.DurationMS = time.Since(start).Milliseconds()
	report.Status = "fail"
	if report.Summary.Passed == len(suites) {
		report.Status = "pass"
	}
	return report
}

func WriteJSON(output io.Writer, report Report) error {
	return json.NewEncoder(output).Encode(report)
}

func WriteText(output io.Writer, report Report) error {
	for _, item := range report.Results {
		if _, err := fmt.Fprintf(output, "%-5s  %-16s  %7.1fs\n",
			strings.ToUpper(item.Status), item.ID, float64(item.DurationMS)/1000); err != nil {
			return err
		}
		for _, failure := range item.FailedCases {
			if _, err := fmt.Fprintf(output, "         %s: %s\n", strings.ToUpper(failure.Kind), failure.ID); err != nil {
				return err
			}
		}
		if item.CasesTruncated {
			if _, err := fmt.Fprintln(output, "         (additional failed cases omitted)"); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintf(output, "Hermetic: %d passed, %d failed, %d errors\n",
		report.Summary.Passed, report.Summary.Failed, report.Summary.Errors)
	if err != nil {
		return err
	}
	for _, item := range report.Results {
		if item.Status != "pass" {
			if _, err := fmt.Fprintf(output, "  To diagnose %s: %s\n", item.ID, item.Rerun); err != nil {
				return err
			}
		}
	}
	return nil
}
