package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
)

const phaseOneEnvironmentID = "11111111-1111-4111-8111-111111111111"

type phaseOneVerifier struct{ binding environment.Binding }
func (v phaseOneVerifier) VerifyProject(_ context.Context, project string) (environment.Binding, error) {
	if project != v.binding.Project { return environment.Binding{}, context.Canceled }
	return v.binding, nil
}

type phaseOneEnrollment struct{ descriptor environment.Descriptor }
func (e phaseOneEnrollment) Enrolled(_ context.Context, binding environment.Binding) ([]environment.Descriptor, error) {
	if binding != e.descriptor.Binding { return nil, context.Canceled }
	return []environment.Descriptor{e.descriptor}, nil
}

type phaseOnePublished struct{}
func (phaseOnePublished) ReadAccepted(context.Context, environment.Descriptor) (*environment.AcceptedRevision, *environment.Candidate, error) {
	return nil, nil, nil
}

type phaseOneConfigured struct{}
func (phaseOneConfigured) ReadConfigured(context.Context, environment.Descriptor) (environment.Candidate, error) {
	return environment.Candidate{}, context.Canceled
}

func TestPhaseOneReadCapabilityCompositionPreservesFailClosedCLI(t *testing.T) {
	if bundle, err := NewEnvironmentReadCapabilities(environment.Catalog{}, nil, nil); err == nil ||
		bundle.Environments != nil || bundle.Accepted != nil {
		t.Fatalf("accepted missing authority: %+v err=%v", bundle, err)
	}
	binding := environment.Binding{ServerID: "verified-server", Project: "team"}
	catalog := environment.Catalog{
		Verifier: phaseOneVerifier{binding: binding},
		Source: phaseOneEnrollment{descriptor: environment.Descriptor{
			ID: phaseOneEnvironmentID, Name: "dev", Binding: binding,
		}},
	}
	readers, err := NewEnvironmentReadCapabilities(catalog, phaseOnePublished{}, phaseOneConfigured{})
	if err != nil { t.Fatal(err) }
	for _, tc := range []struct{args []string; fragment string}{
		{[]string{"--incus-project","team","environment","list","-o","json"}, phaseOneEnvironmentID},
		{[]string{"--incus-project","team","environment","show",phaseOneEnvironmentID,"-o","json"}, phaseOneEnvironmentID},
		{[]string{"--incus-project","team","--environment","dev","state","show","-o","json"}, "\"hasAccepted\": false"},
	} {
		var stdout, stderr bytes.Buffer
		status := RunWithReadCapabilities(context.Background(), tc.args, &stdout, &stderr, nil, nil, nil, readers)
		if status != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), tc.fragment) {
			t.Fatalf("read command %v: status=%d stdout=%s stderr=%s", tc.args, status, stdout.String(), stderr.String())
		}
	}
	// This bundle intentionally does not synthesize a logical workload
	// reader from a candidate or from Incus instance labels.
	for _, args := range [][]string{
		{"--incus-project","foreign","environment","list","-o","json"},
		{"--incus-project","team","--environment","dev","workloads","list","-o","json"},
		{"--incus-project","team","--environment","dev","state","diff","-o","json"},
	} {
		var stdout, stderr bytes.Buffer
		status := RunWithReadCapabilities(context.Background(), args, &stdout, &stderr, nil, nil, nil, readers)
		if status == 0 || stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("unverified read escaped: args=%v status=%d stdout=%s stderr=%s", args, status, stdout.String(), stderr.String())
		}
	}
}
