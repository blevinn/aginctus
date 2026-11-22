package environment

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/pkg/client"
)

func TestVerifiedConfiguredReadModelComposesWithGoClient(t *testing.T) {
	model, verifier, _, candidate := acceptedModelFixture()
	configured := &testConfiguredCandidateSource{c: candidate}
	reader := VerifiedConfiguredReadModel{Catalog: model.Catalog, Configured: configured}
	service := client.New(client.Options{ConfiguredStateReader: reader}).State
	result, err := service.Configured(context.Background(), client.GetConfiguredStateRequest{
		Project: candidate.Environment.Binding.Project, EnvironmentID: candidate.Environment.ID,
	})
	if err != nil || result.Layer != "configured" || result.EnvironmentID != candidate.Environment.ID ||
		len(result.Items) != len(candidate.SelectedWorkloads) || configured.calls != 1 || verifier.calls < 2 {
		t.Fatalf("configured=%+v err=%v calls=%d verify=%d", result, err, configured.calls, verifier.calls)
	}
}

func TestVerifiedConfiguredReadModelDeniesForeignAndBrokenSources(t *testing.T) {
	model, verifier, _, candidate := acceptedModelFixture()
	configured := &testConfiguredCandidateSource{c: candidate}
	reader := VerifiedConfiguredReadModel{Catalog: model.Catalog, Configured: configured}
	project, id := candidate.Environment.Binding.Project, candidate.Environment.ID

	for _, tc := range []struct{ name, project, id string }{
		{"missing project", "", id},
		{"foreign project", "foreign", id},
		{"display name", project, "dev"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := configured.calls
			if result, err := reader.GetConfiguredWorkloads(context.Background(), tc.project, tc.id); err == nil || result.Kind != "" || configured.calls != before {
				t.Fatalf("unverified read=%+v err=%v", result, err)
			}
		})
	}
	var noContext context.Context
	if result, err := reader.GetConfiguredWorkloads(noContext, project, id); err == nil || result.Kind != "" {
		t.Fatalf("nil context=%+v err=%v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := reader.GetConfiguredWorkloads(ctx, project, id); !errors.Is(err, context.Canceled) || result.Kind != "" {
		t.Fatalf("canceled read=%+v err=%v", result, err)
	}
	configured.err = errors.New("private configured filesystem path")
	if result, err := reader.GetConfiguredWorkloads(context.Background(), project, id); err == nil || result.Kind != "" || strings.Contains(err.Error(), "filesystem") {
		t.Fatalf("backend error leaked: %+v err=%v", result, err)
	}
	configured.err = nil
	foreign := candidate
	foreign.Environment.Binding.Project = "foreign"
	configured.c = foreign
	if result, err := reader.GetConfiguredWorkloads(context.Background(), project, id); err == nil || result.Kind != "" {
		t.Fatalf("foreign candidate projected: %+v err=%v", result, err)
	}
	configured.c = candidate
	verifier.binding.Project = "foreign"
	if result, err := reader.GetConfiguredWorkloads(context.Background(), project, id); err == nil || result.Kind != "" {
		t.Fatalf("retargeted verifier permitted: %+v err=%v", result, err)
	}
}
