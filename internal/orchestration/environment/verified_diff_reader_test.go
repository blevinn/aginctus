package environment

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type testConfiguredCandidateSource struct {
	c     Candidate
	err   error
	calls int
}

func (s *testConfiguredCandidateSource) ReadConfigured(_ context.Context, _ Descriptor) (Candidate, error) {
	s.calls++
	return s.c, s.err
}
func TestVerifiedConfiguredDiffReaderPreservesAcceptedToConfiguredProvenance(t *testing.T) {
	model, verifier, _, candidate := acceptedModelFixture()
	source := &testConfiguredCandidateSource{c: candidate}
	reader := VerifiedConfiguredDiffReader{Model: model, Configured: source}
	diff, err := reader.DiffConfiguredAccepted(context.Background(), candidate.Environment.Binding.Project, candidate.Environment.ID)
	if err != nil || diff.Kind != "ConfiguredAcceptedDiff" || diff.FromLayer != "accepted" ||
		diff.ToLayer != "configured" || !diff.Changed || diff.HasAccepted || len(diff.NewlySelected) != 2 {
		t.Fatalf("read-only diff=%+v err=%v", diff, err)
	}
	if verifier.calls < 3 {
		t.Fatalf("binding not reverified: %d", verifier.calls)
	}
	source.err = errors.New("secret-candidate-path")
	if _, err := reader.DiffConfiguredAccepted(context.Background(), candidate.Environment.Binding.Project, candidate.Environment.ID); err == nil || strings.Contains(err.Error(), "secret-candidate-path") {
		t.Fatalf("backend error leaked: %v", err)
	}
	source.err = nil
	foreign := candidate
	foreign.Environment.ID = "22222222-2222-4222-8222-222222222222"
	source.c = foreign
	if value, err := reader.DiffConfiguredAccepted(context.Background(), candidate.Environment.Binding.Project, candidate.Environment.ID); err == nil || value.Kind != "" {
		t.Fatalf("foreign configured candidate accepted: %+v %v", value, err)
	}
}
func TestVerifiedConfiguredDiffReaderDeniesAbsentAuthority(t *testing.T) {
	model, verifier, _, candidate := acceptedModelFixture()
	source := &testConfiguredCandidateSource{c: candidate}
	reader := VerifiedConfiguredDiffReader{Model: model, Configured: source}
	if _, err := reader.DiffConfiguredAccepted(context.Background(), "other", candidate.Environment.ID); err == nil || source.calls != 0 {
		t.Fatalf("read unverified project: err=%v calls=%d", err, source.calls)
	}
	verifier.binding.Project = "other"
	if _, err := reader.DiffConfiguredAccepted(context.Background(), candidate.Environment.Binding.Project, candidate.Environment.ID); err == nil {
		t.Fatal("stale binding allowed")
	}
	reader.Configured = nil
	if _, err := reader.DiffConfiguredAccepted(context.Background(), candidate.Environment.Binding.Project, candidate.Environment.ID); err == nil {
		t.Fatal("missing source allowed")
	}
}
