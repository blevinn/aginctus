package environment

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExplicitConfiguredCandidateFileUsesExactEnrolledEnvironment(t *testing.T) {
	c := candidateFixture()
	body, err := c.Canonical()
	if err != nil { t.Fatal(err) }
	path := filepath.Join(t.TempDir(), "candidate.json")
	if err := os.WriteFile(path, body, 0o600); err != nil { t.Fatal(err) }
	src := ExplicitConfiguredCandidateFile{AbsolutePath: path}
	got, err := src.ReadConfigured(context.Background(), c.Environment)
	if err != nil || !got.Environment.SameEnvironment(c.Environment) ||
		len(got.Workloads) != len(c.Workloads) {
		t.Fatalf("configured candidate=%+v err=%v", got, err)
	}
	foreign := c.Environment
	foreign.Binding.Project = "foreign"
	if got, err := src.ReadConfigured(context.Background(), foreign); err == nil ||
		got.Environment.ID != "" {
		t.Fatalf("foreign scope accepted: %+v %v", got, err)
	}
	foreign = c.Environment
	foreign.ID = secondID
	if got, err := src.ReadConfigured(context.Background(), foreign); err == nil ||
		got.Environment.ID != "" {
		t.Fatalf("foreign environment accepted: %+v %v", got, err)
	}
	src.AbsolutePath = "relative.json"
	if _, err := src.ReadConfigured(context.Background(), c.Environment); err == nil {
		t.Fatal("relative source accepted")
	}
	src.AbsolutePath = filepath.Join(t.TempDir(), "missing")
	if _, err := src.ReadConfigured(context.Background(), c.Environment); err == nil {
		t.Fatal("missing source treated as empty state")
	}
}
