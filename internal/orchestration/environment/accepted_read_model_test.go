package environment

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
	"github.com/blevinn/aginctus/pkg/api"
)

type acceptedSourceFixture struct {
	revision  *AcceptedRevision
	candidate *Candidate
	err       error
	calls     int
}

func (s *acceptedSourceFixture) ReadAccepted(_ context.Context, _ Descriptor) (*AcceptedRevision, *Candidate, error) {
	s.calls++
	return s.revision, s.candidate, s.err
}

func acceptedModelFixture() (AcceptedReadModel, *testProjectVerifier, *acceptedSourceFixture, Candidate) {
	c := candidateFixture()
	verifier := &testProjectVerifier{binding: c.Environment.Binding}
	enrolled := &testEnrollmentSource{descriptors: []Descriptor{c.Environment}}
	source := &acceptedSourceFixture{}
	return AcceptedReadModel{Catalog: Catalog{Verifier: verifier, Source: enrolled}, Source: source}, verifier, source, c
}

func TestAcceptedReadModelAuthenticatesBeforeReportingAbsence(t *testing.T) {
	model, verifier, source, c := acceptedModelFixture()
	ctx := context.Background()
	got, err := model.GetAcceptedState(ctx, c.Environment.Binding.Project, c.Environment.ID)
	if err != nil || got.Kind != "AcceptedStateStatus" || got.APIVersion != api.APIVersionV1Alpha1 ||
		got.Environment.ID != c.Environment.ID || got.HasAccepted || got.Revision != nil {
		t.Fatalf("verified absence: %+v %v", got, err)
	}
	if source.calls != 1 || verifier.calls != 2 {
		t.Fatal("unverified reader invoked")
	}
	source.err = errors.New("secret-root-password")
	if got, err := model.GetAcceptedState(ctx, c.Environment.Binding.Project, c.Environment.ID); err == nil ||
		got.HasAccepted || strings.Contains(err.Error(), "secret-root-password") {
		t.Fatalf("backend failure appeared as no accepted revision or leaked secret: %+v %v", got, err)
	}
	source.err = nil
	verifier.binding.Project = "foreign"
	calls := source.calls
	if _, err := model.GetAcceptedState(ctx, c.Environment.Binding.Project, c.Environment.ID); err == nil || source.calls != calls {
		t.Fatalf("foreign binding was used for accepted-state read: %v", err)
	}
	if _, err := model.GetAcceptedState(ctx, "", c.Environment.ID); err == nil {
		t.Fatal("ambient project accepted")
	}
	if _, err := model.GetAcceptedState(ctx, c.Environment.Binding.Project, "shared"); err == nil {
		t.Fatal("display name used as UUID")
	}
}

func TestAcceptedReadModelRejectsTamperingAndForeignEnvironment(t *testing.T) {
	model, _, source, c := acceptedModelFixture()
	revision, err := BuildAcceptedRevision(nil, nil, c)
	if err != nil {
		t.Fatal(err)
	}
	source.revision = &revision
	source.candidate = &c
	status, err := model.GetAcceptedState(context.Background(), c.Environment.Binding.Project, c.Environment.ID)
	if err != nil || status.Revision == nil || status.Revision.ID != revision.ID || !status.HasAccepted {
		t.Fatalf("accepted status not reported: %+v %v", status, err)
	}
	tampered := c
	tampered.Workloads = append([]acceptedstate.WorkloadSpec(nil), c.Workloads...)
	tampered.Workloads[0].ImageFingerprint = strings.Repeat("f", 64)
	source.candidate = &tampered
	if _, err := model.GetAcceptedState(context.Background(), c.Environment.Binding.Project, c.Environment.ID); err == nil {
		t.Fatal("published content did not match digest")
	}
	source.candidate = nil
	if _, err := model.GetAcceptedState(context.Background(), c.Environment.Binding.Project, c.Environment.ID); err == nil {
		t.Fatal("missing candidate treated as empty accepted")
	}
	source.candidate = &c
	foreign := c
	foreign.Environment.ID = "7c82e6d1-1212-4123-8123-0123456789ac"
	foreignRev, err := BuildAcceptedRevision(nil, nil, foreign)
	if err != nil {
		t.Fatal(err)
	}
	source.revision = &foreignRev
	source.candidate = &foreign
	if _, err := model.GetAcceptedState(context.Background(), c.Environment.Binding.Project, c.Environment.ID); err == nil {
		t.Fatal("accepted revision for another environment")
	}
}

func TestReadOnlyConfiguredAcceptedDiffContainsNoFieldValues(t *testing.T) {
	model, _, source, c := acceptedModelFixture()
	first, err := BuildAcceptedRevision(nil, nil, c)
	if err != nil {
		t.Fatal(err)
	}
	source.revision = &first
	source.candidate = &c
	changed := c
	changed.Workloads = append([]acceptedstate.WorkloadSpec(nil), c.Workloads...)
	changed.Workloads[0].ImageFingerprint = strings.Repeat("d", 64)
	changed.Environment.Name = "renamed"
	diff, err := model.DiffConfiguredAgainstAccepted(context.Background(), c.Environment.Binding.Project, c.Environment.ID, changed)
	if err != nil || !diff.HasAccepted || !diff.Changed || !diff.DisplayNameChanged ||
		len(diff.Modified) != 1 || diff.Modified[0].Fields[0] != "imageFingerprint" {
		t.Fatalf("missing field-only drift: %+v %v", diff, err)
	}
	jsonText, err := json.Marshal(diff)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(jsonText), strings.Repeat("d", 64)) {
		t.Fatal("diff serialized image fingerprint instead of field name")
	}
	if diff.Kind != "ConfiguredAcceptedDiff" || diff.FromLayer != "accepted" || diff.ToLayer != "configured" {
		t.Fatalf("ambiguous state layers: %+v", diff)
	}
	foreign := changed
	foreign.Environment.Binding.Project = "other"
	if _, err := model.DiffConfiguredAgainstAccepted(context.Background(), c.Environment.Binding.Project, c.Environment.ID, foreign); err == nil {
		t.Fatal("candidate redirected accepted project")
	}
}

func TestReadOnlyFirstAcceptanceDoesNotInventDeletion(t *testing.T) {
	model, _, _, c := acceptedModelFixture()
	diff, err := model.DiffConfiguredAgainstAccepted(context.Background(), c.Environment.Binding.Project, c.Environment.ID, c)
	if err != nil || diff.HasAccepted || !diff.Changed || len(diff.NewlySelected) != 2 ||
		len(diff.Unselected) != 0 || len(diff.Modified) != 0 || diff.Revision != nil {
		t.Fatalf("no accepted revision comparison: %+v %v", diff, err)
	}
}
