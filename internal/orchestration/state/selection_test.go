package acceptedstate

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func fixture(names ...string) SelectionManifest {
	return SelectionManifest{
		APIVersion:        SelectionAPIVersion,
		Kind:              SelectionKind,
		DeploymentID:      "primary",
		IncusProject:      "aginctus",
		SelectedWorkloads: append([]string{}, names...),
	}
}

func TestCanonicalAndDigestAreDeterministic(t *testing.T) {
	first := fixture("z", "a", "m")
	original := append([]string(nil), first.SelectedWorkloads...)
	second := fixture("m", "z", "a")
	a, err := first.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	b, err := second.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("canonical mismatch: %s != %s", a, b)
	}
	if !reflect.DeepEqual(first.SelectedWorkloads, original) {
		t.Fatal("caller slice mutated")
	}
	digest1, err := first.Digest()
	if err != nil {
		t.Fatal(err)
	}
	digest2, err := second.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if digest1 != digest2 || len(digest1) != 64 {
		t.Fatal("non-deterministic digest")
	}
	parsed, err := ParseSelection(a)
	if err != nil || !reflect.DeepEqual(parsed.SelectedWorkloads, []string{"a", "m", "z"}) {
		t.Fatalf("canonical parse: %+v, %v", parsed, err)
	}
}

func TestExplicitEmptySelection(t *testing.T) {
	s := fixture()
	b, err := s.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"selectedWorkloads":[]`) {
		t.Fatalf("empty set not explicit: %s", b)
	}
	s.SelectedWorkloads = nil
	if _, err := s.Canonical(); err == nil {
		t.Fatal("nil selection silently interpreted as empty")
	}
}

func TestRejectInvalidIdentitiesAndSelections(t *testing.T) {
	cases := []struct {
		name string
		edit func(*SelectionManifest)
	}{
		{"default project", func(s *SelectionManifest) { s.IncusProject = "default" }},
		{"blank project", func(s *SelectionManifest) { s.IncusProject = "" }},
		{"unsafe deployment", func(s *SelectionManifest) { s.DeploymentID = "../escape" }},
		{"unsafe workload", func(s *SelectionManifest) { s.SelectedWorkloads = []string{"../x"} }},
		{"duplicate workload", func(s *SelectionManifest) { s.SelectedWorkloads = []string{"a", "a"} }},
		{"unsupported version", func(s *SelectionManifest) { s.APIVersion = "v2" }},
		{"unsupported kind", func(s *SelectionManifest) { s.Kind = "Deployment" }},
		{"too many", func(s *SelectionManifest) { s.SelectedWorkloads = make([]string, 257) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture("dev")
			tc.edit(&s)
			if _, err := s.Canonical(); err == nil {
				t.Fatal("accepted invalid selection")
			}
		})
	}
}

func TestStrictParsingRejectsUnknownDuplicateAndTrailing(t *testing.T) {
	valid, err := fixture("dev").Canonical()
	if err != nil {
		t.Fatal(err)
	}
	cases := []string{
		`{"apiVersion":"aginctus.dev/accepted/v1alpha1","apiVersion":"v2","kind":"SelectionManifest","deploymentID":"primary","incusProject":"aginctus","selectedWorkloads":[]}`,
		`{"apiVersion":"aginctus.dev/accepted/v1alpha1","kind":"SelectionManifest","deploymentID":"primary","incusProject":"aginctus","selectedWorkloads":[],"gateway":{"master":{"key":"sensitive-value"}}}`,
		string(valid) + " {}",
		`null`, `[]`, `{"incusProject":"aginctus"}`,
		`{"ApiVersion":"aginctus.dev/accepted/v1alpha1","kind":"SelectionManifest","deploymentID":"primary","incusProject":"aginctus","selectedWorkloads":[]}`,
	}
	for _, raw := range cases {
		_, err := ParseSelection([]byte(raw))
		if err == nil {
			t.Fatalf("accepted invalid JSON %q", raw)
		}
		if strings.Contains(err.Error(), "sensitive-value") {
			t.Fatal("error disclosed secret")
		}
	}
	if _, err := ParseSelection(make([]byte, maxSelectionBytes+1)); err == nil {
		t.Fatal("accepted oversized JSON")
	}
}

func TestDiffIsReadOnlyAndUnselectionIsNotDeletion(t *testing.T) {
	active, candidate := fixture("c", "a"), fixture("d", "b")
	diff, err := CompareSelections(active, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(diff.NewlySelected, []string{"b", "d"}) || !reflect.DeepEqual(diff.Unselected, []string{"a", "c"}) {
		t.Fatalf("unexpected selection diff: %+v", diff)
	}
	b, err := json.Marshal(diff)
	if err != nil || strings.Contains(string(b), `"delete"`) {
		t.Fatalf("diff implies deletion: %s, %v", b, err)
	}
	candidate.IncusProject = "other"
	if _, err := CompareSelections(active, candidate); err == nil {
		t.Fatal("cross-project comparison should fail")
	}
	candidate = fixture("z")
	candidate.DeploymentID = "other"
	if _, err := CompareSelections(active, candidate); err == nil {
		t.Fatal("cross-deployment comparison should fail")
	}
}

func FuzzParseSelection(f *testing.F) {
	valid, _ := fixture("dev").Canonical()
	f.Add(valid)
	f.Add([]byte(`{"apiVersion":"x","kind":"y"}`))
	f.Fuzz(func(t *testing.T, payload []byte) {
		selection, err := ParseSelection(payload)
		if err != nil {
			return
		}
		canonical, err := selection.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		restored, err := ParseSelection(canonical)
		if err != nil {
			t.Fatal(err)
		}
		again, err := restored.Canonical()
		if err != nil || string(canonical) != string(again) {
			t.Fatal("canonicalization not idempotent")
		}
	})
}
