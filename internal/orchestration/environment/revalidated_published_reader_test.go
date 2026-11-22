package environment

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type changingProjectVerifier struct {
	first Binding
	second Binding
	calls int
	err error
}

func (v *changingProjectVerifier) VerifyProject(_ context.Context, _ string) (Binding, error) {
	v.calls++
	if v.err != nil {
		return Binding{}, v.err
	}
	if v.calls == 1 {
		return v.first, nil
	}
	return v.second, nil
}

func TestRevalidatedReadDetectsProjectRetargetAcrossSourceRead(t *testing.T) {
	c := candidateFixture()
	revision, err := BuildAcceptedRevision(nil, nil, c)
	if err != nil {
		t.Fatal(err)
	}
	verifier := &changingProjectVerifier{first: c.Environment.Binding, second: Binding{
		ServerID: c.Environment.Binding.ServerID, Project: "foreign",
	}}
	source := &acceptedSourceFixture{revision: &revision, candidate: &c}
	reader := RevalidatedPublishedReader{
		Catalog: Catalog{Verifier: verifier, Source: &testEnrollmentSource{
			descriptors: []Descriptor{c.Environment},
		}},
		Source: source,
	}
	if rev, body, err := reader.Read(context.Background(), c.Environment.Binding.Project, c.Environment.ID); err == nil || rev != nil || body != nil {
		t.Fatal("published read succeeded after project identity changed")
	}
	if source.calls != 0 || verifier.calls != 2 {
		t.Fatalf("catalog post-check must block source read: source=%d verification=%d", source.calls, verifier.calls)
	}
	verifier.calls = 0
	verifier.second = verifier.first
	rev, body, err := reader.Read(context.Background(), c.Environment.Binding.Project, c.Environment.ID)
	if err != nil || rev == nil || rev.ID != revision.ID || body == nil {
		t.Fatalf("stable verified scope refused: %v", err)
	}
}

func TestRevalidatedReadRejectsForgedScopeAndBackendErrors(t *testing.T) {
	c := candidateFixture()
	verifier := &changingProjectVerifier{first: c.Environment.Binding, second: c.Environment.Binding}
	source := &acceptedSourceFixture{}
	reader := RevalidatedPublishedReader{
		Catalog: Catalog{Verifier: verifier, Source: &testEnrollmentSource{
			descriptors: []Descriptor{c.Environment},
		}},
		Source: source,
	}
	for _, selection := range []struct{ project, id string }{
		{"", c.Environment.ID}, {"default", c.Environment.ID}, {"other", c.Environment.ID},
		{c.Environment.Binding.Project, "development"},
	} {
		verifier.calls = 0
		calls := source.calls
		if _, _, err := reader.Read(context.Background(), selection.project, selection.id); err == nil {
			t.Fatalf("accepted invalid selection: %+v", selection)
		}
		if source.calls != calls {
			t.Fatal("source read happened before exact project/environment verification")
		}
	}
	verifier.calls = 0
	source.err = errors.New("private transport key material")
	if _, _, err := reader.Read(context.Background(), c.Environment.Binding.Project, c.Environment.ID); err == nil ||
		strings.Contains(err.Error(), "private transport key material") {
		t.Fatal("backend error leaked or was interpreted as absence")
	}
	source.err = nil
	verifier.calls = 0
	if rev, body, err := reader.Read(context.Background(), c.Environment.Binding.Project, c.Environment.ID); err != nil || rev != nil || body != nil {
		t.Fatalf("authenticated empty state not preserved: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := source.calls
	if _, _, err := reader.Read(ctx, c.Environment.Binding.Project, c.Environment.ID); !errors.Is(err, context.Canceled) || source.calls != calls {
		t.Fatal("cancellation did not stop read before source")
	}
}
