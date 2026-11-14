package orchestration_test

import (
	"strings"
	"testing"
	"time"

	"github.com/blevinn/aginctus/pkg/orchestration"
)

func criterion() orchestration.Check {
	return orchestration.Check{
		ID: "upstream", Claim: "selected model answers a probe", Scope: "selected route and model", Required: true,
		Effect: orchestration.ActiveProbe,
		Access: []orchestration.Access{orchestration.NetworkAccess, orchestration.CredentialAccess, orchestration.ModelAccess},
		OptIns: []string{"live-inference"}, Timeout: time.Second, RetryInterval: time.Millisecond,
	}
}

func TestCheckRequirements(t *testing.T) {
	if err := criterion().Validate(); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		change func(*orchestration.Check)
	}{
		{"zero effect", func(c *orchestration.Check) { c.Effect = "" }},
		{"offline external access", func(c *orchestration.Check) { c.Effect = orchestration.Offline }},
		{"model disguised as read", func(c *orchestration.Check) { c.Effect = orchestration.ReadOnly }},
		{"missing probe opt-in", func(c *orchestration.Check) { c.OptIns = nil }},
		{"no declared access", func(c *orchestration.Check) { c.Access = nil }},
		{"unknown access", func(c *orchestration.Check) { c.Access = []orchestration.Access{"future"} }},
		{"duplicate access", func(c *orchestration.Check) { c.Access = append(c.Access, orchestration.NetworkAccess) }},
		{"duplicate opt-in", func(c *orchestration.Check) { c.OptIns = append(c.OptIns, c.OptIns[0]) }},
		{"missing evidence claim", func(c *orchestration.Check) { c.Claim = "" }},
		{"unbounded evaluation", func(c *orchestration.Check) { c.Timeout = 0 }},
		{"retry outside deadline", func(c *orchestration.Check) { c.RetryInterval = c.Timeout }},
		{"negative freshness", func(c *orchestration.Check) { c.MaxAge = -time.Second }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := criterion()
			tt.change(&check)
			if err := check.Validate(); err == nil {
				t.Fatal("invalid requirements accepted")
			}
		})
	}
	offline := orchestration.Check{ID: "shape", Claim: "configuration is valid", Scope: "candidate only", Effect: orchestration.Offline, Timeout: time.Second}
	if err := offline.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestFindingsPreserveEvidenceOutcomes(t *testing.T) {
	for _, outcome := range []orchestration.Outcome{orchestration.Passed, orchestration.Failed, orchestration.Unknown, orchestration.Skipped} {
		finding := orchestration.Finding{TargetID: "service", CheckID: "upstream", Outcome: outcome, ObservedAt: time.Now(), Code: "route-probe"}
		if err := finding.Validate(); err != nil {
			t.Fatal(err)
		}
		if finding.Outcome != outcome {
			t.Fatal("validation rewrote outcome")
		}
	}
	base := orchestration.Finding{TargetID: "service", CheckID: "ready", Outcome: orchestration.Passed, ObservedAt: time.Now(), Code: "ready"}
	tests := []struct {
		name   string
		change func(*orchestration.Finding)
	}{
		{"zero outcome", func(f *orchestration.Finding) { f.Outcome = "" }},
		{"unknown outcome", func(f *orchestration.Finding) { f.Outcome = "future" }},
		{"no correlation", func(f *orchestration.Finding) { f.CheckID = "" }},
		{"no time", func(f *orchestration.Finding) { f.ObservedAt = time.Time{} }},
		{"no diagnostic code", func(f *orchestration.Finding) { f.Code = "" }},
		{"unbounded payload", func(f *orchestration.Finding) { f.Message = strings.Repeat("a", 2049) }},
		{"terminal escape", func(f *orchestration.Finding) { f.Remediation = "\x1b[0m" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			finding := base
			tt.change(&finding)
			if err := finding.Validate(); err == nil {
				t.Fatal("invalid finding accepted")
			}
		})
	}
}
