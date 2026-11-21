package environment

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type testProjectVerifier struct {
	binding Binding
	calls   int
	err     error
}

func (v *testProjectVerifier) VerifyProject(_ context.Context, project string) (Binding, error) {
	v.calls++
	if v.err != nil {
		return Binding{}, v.err
	}
	return v.binding, nil
}

type testEnrollmentSource struct {
	descriptors []Descriptor
	calls       int
	err         error
}

func (s *testEnrollmentSource) Enrolled(_ context.Context, b Binding) ([]Descriptor, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.descriptors, nil
}
func testDescriptor(id string) Descriptor {
	return Descriptor{
		ID: id, Name: "shared", Binding: Binding{ServerID: "verified-server", Project: "aginctus"},
	}
}

const firstID = "11111111-1111-4111-8111-111111111111"
const secondID = "22222222-2222-4222-8222-222222222222"

func TestCatalogTwoEnvironmentsOneProjectAndStableOrdering(t *testing.T) {
	v := &testProjectVerifier{binding: Binding{ServerID: "verified-server", Project: "aginctus"}}
	s := &testEnrollmentSource{descriptors: []Descriptor{testDescriptor(secondID), testDescriptor(firstID)}}
	reader := Catalog{Verifier: v, Source: s}
	got, err := reader.List(context.Background(), "aginctus")
	if err != nil || len(got) != 2 || got[0].ID != firstID || got[1].ID != secondID {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	d, err := reader.Get(context.Background(), "aginctus", secondID)
	if err != nil || d.ID != secondID {
		t.Fatalf("get=%#v err=%v", d, err)
	}
	if _, err := reader.Get(context.Background(), "aginctus", "shared"); err == nil {
		t.Fatal("display name selected a resource")
	}
}

func TestCatalogFailsClosedBeforeDisclosure(t *testing.T) {
	base := testDescriptor(firstID)
	for _, tc := range []struct {
		name    string
		binding Binding
		records []Descriptor
		err     error
		project string
	}{
		{"wrong scope", Binding{ServerID: "verified-server", Project: "other"}, []Descriptor{base}, nil, "aginctus"},
		{"no project", base.Binding, []Descriptor{base}, nil, ""},
		{"source failure", base.Binding, []Descriptor{base}, errors.New("sensitive source error"), "aginctus"},
		{"nil inventory", base.Binding, nil, nil, "aginctus"},
		{"foreign", base.Binding, []Descriptor{base, {ID: secondID, Name: "foreign", Binding: Binding{ServerID: "other-server", Project: "aginctus"}}}, nil, "aginctus"},
		{"duplicate", base.Binding, []Descriptor{base, base}, nil, "aginctus"},
		{"invalid", base.Binding, []Descriptor{base, {ID: "malformed", Name: "x", Binding: base.Binding}}, nil, "aginctus"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := &testProjectVerifier{binding: tc.binding}
			s := &testEnrollmentSource{descriptors: tc.records, err: tc.err}
			got, err := (Catalog{Verifier: v, Source: s}).List(context.Background(), tc.project)
			if err == nil || len(got) != 0 {
				t.Fatalf("records leaked: %#v err=%v", got, err)
			}
			if strings.Contains(err.Error(), "sensitive") {
				t.Fatal("untrusted source error leaked")
			}
			if tc.project == "" || tc.name == "wrong scope" {
				if s.calls != 0 {
					t.Fatal("source queried without authenticated scope")
				}
			}
		})
	}
	v := &testProjectVerifier{binding: base.Binding}
	s := &testEnrollmentSource{descriptors: []Descriptor{base}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Catalog{Verifier: v, Source: s}).List(ctx, "aginctus"); !errors.Is(err, context.Canceled) || v.calls != 0 {
		t.Fatalf("cancellation: %v", err)
	}
}
