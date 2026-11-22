package environment

import (
	"context"
	"errors"
	"testing"

	"github.com/blevinn/aginctus/pkg/client"
)

func TestCatalogClientReaderProvidesTypedEnvironmentReads(t *testing.T) {
	binding := testDescriptor(firstID).Binding
	verifier := &testProjectVerifier{binding: binding}
	source := &testEnrollmentSource{descriptors: []Descriptor{
		testDescriptor(secondID), testDescriptor(firstID),
	}}
	reader := CatalogClientReader{Catalog: Catalog{Verifier: verifier, Source: source}}
	service := client.New(client.Options{EnvironmentReader: reader}).Environments

	list, err := service.List(context.Background(), client.ListEnvironmentsRequest{Project: binding.Project})
	if err != nil || len(list.Items) != 2 || list.Items[0].ID != firstID ||
		list.Items[1].ID != secondID || list.Items[0].Binding.ServerID != binding.ServerID {
		t.Fatalf("typed inventory=%+v err=%v", list, err)
	}
	detail, err := service.Get(context.Background(), client.GetEnvironmentRequest{
		Project: binding.Project, ID: secondID,
	})
	if err != nil || detail.Environment.ID != secondID || detail.Environment.Binding.Project != binding.Project {
		t.Fatalf("typed detail=%+v err=%v", detail, err)
	}
	if _, err := service.Get(context.Background(), client.GetEnvironmentRequest{
		Project: binding.Project, ID: "shared",
	}); err == nil {
		t.Fatal("name used as immutable identity")
	}
}

func TestCatalogClientReaderFailsClosedWithoutAuthority(t *testing.T) {
	binding := testDescriptor(firstID).Binding
	verifier := &testProjectVerifier{binding: binding}
	source := &testEnrollmentSource{descriptors: []Descriptor{testDescriptor(firstID)}}
	reader := CatalogClientReader{Catalog: Catalog{Verifier: verifier, Source: source}}
	for _, tc := range []struct{ name string; project string }{
		{"missing project", ""}, {"foreign project", "foreign"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if result, err := reader.ListEnvironments(context.Background(), tc.project); err == nil || result.Kind != "" {
				t.Fatalf("unverified inventory=%+v err=%v", result, err)
			}
		})
	}
	if result, err := reader.ListEnvironments(nil, binding.Project); err == nil || result.Kind != "" {
		t.Fatalf("nil-context inventory=%+v err=%v", result, err)
	}
	if result, err := reader.GetEnvironment(nil, binding.Project, firstID); err == nil || result.Kind != "" {
		t.Fatalf("nil-context detail=%+v err=%v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := reader.GetEnvironment(ctx, binding.Project, firstID); !errors.Is(err, context.Canceled) || result.Kind != "" {
		t.Fatalf("canceled detail=%+v err=%v", result, err)
	}
	verifier.err = errors.New("private authorization detail")
	if result, err := reader.ListEnvironments(context.Background(), binding.Project); err == nil || result.Kind != "" {
		t.Fatalf("unauthorized inventory=%+v err=%v", result, err)
	}
}
