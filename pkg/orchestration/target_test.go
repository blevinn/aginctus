package orchestration_test

import (
	"strings"
	"testing"
	"time"

	"github.com/blevinn/aginctus/pkg/orchestration"
)

func target() orchestration.Target {
	return orchestration.Target{
		ID: "service", Capability: "gateway", DependsOn: []string{"catalog"},
		Containers: []orchestration.Container{
			{ID: "proxy", Image: orchestration.ImageReference{Format: orchestration.IncusImage, Pointer: "images:custom-proxy"},
				Contract: orchestration.GuestContract{ID: "proxy", Version: "v1", Requirements: []string{"Linux guest with helper protocol v1"}}},
			{ID: "database", Image: orchestration.ImageReference{Format: orchestration.OCIImage, Pointer: "registry.example/db@sha256:" + strings.Repeat("a", 64)},
				Contract: orchestration.GuestContract{ID: "database", Version: "v2", Requirements: []string{"Database service with health endpoint"}}},
		},
		Postconditions: []orchestration.Check{{
			ID: "ready", Claim: "guest helper reports ready", Scope: "selected proxy guest", Required: true,
			Effect: orchestration.ReadOnly, Access: []orchestration.Access{orchestration.GuestAccess}, Timeout: time.Second,
		}},
	}
}

func TestContainerAndContainerFreeContributions(t *testing.T) {
	target := target()
	if err := target.Validate(); err != nil {
		t.Fatal(err)
	}
	if target.Containers[0].Image.Pointer != "images:custom-proxy" || target.Containers[1].ID != "database" {
		t.Fatal("validation changed configured images or ordering")
	}
	target.Containers = nil
	target.DependsOn = nil
	target.Postconditions = nil
	if err := target.Validate(); err != nil {
		t.Fatalf("container-free target rejected: %v", err)
	}
}

func TestTargetRejectsAmbiguousDeclarations(t *testing.T) {
	tests := []struct {
		name   string
		change func(*orchestration.Target)
	}{
		{"invalid identity", func(t *orchestration.Target) { t.ID = "../service" }},
		{"no capability", func(t *orchestration.Target) { t.Capability = "" }},
		{"self dependency", func(t *orchestration.Target) { t.DependsOn = []string{t.ID} }},
		{"duplicate dependency", func(t *orchestration.Target) { t.DependsOn = []string{"catalog", "catalog"} }},
		{"duplicate container", func(t *orchestration.Target) { t.Containers[1].ID = t.Containers[0].ID }},
		{"missing image", func(t *orchestration.Target) { t.Containers[0].Image.Pointer = "" }},
		{"image control character", func(t *orchestration.Target) { t.Containers[0].Image.Pointer = "image\nnext" }},
		{"image whitespace", func(t *orchestration.Target) { t.Containers[0].Image.Pointer = "image alias" }},
		{"unknown image format", func(t *orchestration.Target) { t.Containers[0].Image.Format = "future" }},
		{"missing guest contract", func(t *orchestration.Target) { t.Containers[0].Contract.ID = "" }},
		{"missing contract version", func(t *orchestration.Target) { t.Containers[0].Contract.Version = "" }},
		{"no guest requirements", func(t *orchestration.Target) { t.Containers[0].Contract.Requirements = nil }},
		{"blank requirement", func(t *orchestration.Target) { t.Containers[0].Contract.Requirements = []string{" "} }},
		{"duplicate phase check", func(t *orchestration.Target) { t.Preconditions = t.Postconditions }},
		{"invalid criterion", func(t *orchestration.Target) { t.Postconditions[0].Scope = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := target()
			tt.change(&target)
			if err := target.Validate(); err == nil {
				t.Fatal("invalid contribution accepted")
			}
		})
	}
}
