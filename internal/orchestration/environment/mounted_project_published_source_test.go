package environment

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestMountedProjectPublishedSourcePreservesAcceptedAndAbsence(t *testing.T) {
	candidate := candidateFixture()
	root := filepath.Join(t.TempDir(), "state")
	store, err := InitializeLocalProjectStateStore(root, candidate.Environment.Binding)
	if err != nil { t.Fatal(err) }
	verifier := &testProjectVerifier{binding: candidate.Environment.Binding}
	checks := 0
	source := &MountedProjectPublishedSource{
		store: store, root: root, binding: candidate.Environment.Binding,
		verifier: verifier, checkMount: func(string) error { checks++; return nil },
	}
	rev, body, err := source.ReadAccepted(context.Background(), candidate.Environment)
	if err != nil || rev != nil || body != nil || checks != 2 || verifier.calls != 2 {
		t.Fatalf("verified absence: rev=%+v body=%+v err=%v checks=%d auth=%d",
			rev, body, err, checks, verifier.calls)
	}
	expected, err := store.CommitLocalCandidate(context.Background(),
		candidate.Environment, candidate.Environment.Binding, nil, candidate)
	if err != nil { t.Fatal(err) }
	rev, body, err = source.ReadAccepted(context.Background(), candidate.Environment)
	if err != nil || rev == nil || rev.ID != expected.ID ||
		body == nil || rev.ValidateAgainst(*body) != nil {
		t.Fatalf("published revision: rev=%+v body=%+v err=%v", rev, body, err)
	}
}

func TestMountedProjectPublishedSourceFailsClosedOnMountAndAuthorityChange(t *testing.T) {
	candidate := candidateFixture()
	root := filepath.Join(t.TempDir(), "state")
	store, err := InitializeLocalProjectStateStore(root, candidate.Environment.Binding)
	if err != nil { t.Fatal(err) }
	verifier := &testProjectVerifier{binding: candidate.Environment.Binding}
	mountOK := true
	source := &MountedProjectPublishedSource{
		store: store, root: root, binding: candidate.Environment.Binding,
		verifier: verifier,
		checkMount: func(string) error {
			if !mountOK { return errors.New("private mount path") }
			return nil
		},
	}
	mountOK = false
	if rev, body, err := source.ReadAccepted(context.Background(), candidate.Environment);
		err == nil || rev != nil || body != nil {
		t.Fatalf("missing mount produced accepted result: %+v %+v %v", rev, body, err)
	}
	mountOK = true
	verifier.binding.Project = "foreign"
	if rev, body, err := source.ReadAccepted(context.Background(), candidate.Environment);
		err == nil || rev != nil || body != nil {
		t.Fatalf("retargeted authority produced accepted result: %+v %+v %v", rev, body, err)
	}
	foreign := candidate.Environment
	foreign.ID = secondID
	if rev, body, err := source.ReadAccepted(context.Background(), foreign);
		err == nil || rev != nil || body != nil {
		t.Fatalf("foreign environment produced accepted result: %+v %+v %v", rev, body, err)
	}
}

func TestMountedProjectSourceRefusesOrdinaryFixtureDirectory(t *testing.T) {
	candidate := candidateFixture()
	root := filepath.Join(t.TempDir(), "ordinary")
	_, err := InitializeLocalProjectStateStore(root, candidate.Environment.Binding)
	if err != nil { t.Fatal(err) }
	if err := checkProjectStateMountpoint(root); err == nil {
		t.Fatal("same-UID fixture directory masqueraded as a separate mounted volume")
	}
	if source, err := OpenMountedProjectPublishedSource(context.Background(), root,
		candidate.Environment.Binding.Project, &testProjectVerifier{binding: candidate.Environment.Binding});
		err == nil || source != nil {
		t.Fatalf("unattested state root accepted: %+v err=%v", source, err)
	}
}
