package acceptedstate

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func validSpec() DeploymentSpec {
	return DeploymentSpec{
		APIVersion: DeploymentAPIVersion,
		Kind:       DeploymentKind,
		Selection: SelectionManifest{
			APIVersion: SelectionAPIVersion, Kind: SelectionKind,
			DeploymentID: "demo", IncusProject: "aginctus",
			SelectedWorkloads: []string{"beta", "alpha"},
		},
		Workloads: []WorkloadSpec{
			{ID: "beta", InstanceName: "beta", InstanceType: "container",
				ImageFingerprint: strings.Repeat("b", 64), Profiles: []string{"base"}, EnsureRunning: true},
			{ID: "alpha", InstanceName: "alpha", InstanceType: "virtual-machine",
				ImageFingerprint: strings.Repeat("a", 64), Profiles: []string{}, EnsureRunning: false},
		},
	}
}

func TestDeploymentCanonicalAndParser(t *testing.T) {
	s := validSpec()
	a, err := s.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if s.Workloads[0].ID != "beta" || s.Selection.SelectedWorkloads[0] != "beta" {
		t.Fatal("canonicalization mutated caller")
	}
	if !bytes.Contains(a, []byte(`"workloads":[{"id":"alpha"`)) {
		t.Fatal(string(a))
	}
	parsed, err := ParseDeploymentSpec(a)
	if err != nil {
		t.Fatal(err)
	}
	b, err := parsed.Canonical()
	if err != nil || !bytes.Equal(a, b) {
		t.Fatalf("unstable canonical form: %v", err)
	}
	d1, _ := s.Digest()
	d2, _ := parsed.Digest()
	if d1 != d2 || len(d1) != 64 {
		t.Fatal("unstable digest")
	}
	s.Workloads[0].Profiles[0] = "changed"
	d3, _ := s.Digest()
	if d3 == d1 {
		t.Fatal("profile changes should affect digest")
	}
}

func TestDeploymentRejectsUnsafeOrIncompleteIntent(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*DeploymentSpec)
	}{
		{"missing workload", func(s *DeploymentSpec) { s.Workloads = s.Workloads[:1] }},
		{"unknown workload", func(s *DeploymentSpec) { s.Workloads[0].ID = "other" }},
		{"duplicate workload", func(s *DeploymentSpec) { s.Workloads[0].ID = "alpha" }},
		{"implicit profiles", func(s *DeploymentSpec) { s.Workloads[0].Profiles = nil }},
		{"profile duplicate", func(s *DeploymentSpec) { s.Workloads[0].Profiles = []string{"x", "x"} }},
		{"unpinned image", func(s *DeploymentSpec) { s.Workloads[0].ImageFingerprint = "latest" }},
		{"uppercase fingerprint", func(s *DeploymentSpec) { s.Workloads[0].ImageFingerprint = strings.Repeat("A", 64) }},
		{"invalid instance", func(s *DeploymentSpec) { s.Workloads[0].InstanceName = "../host" }},
		{"shared instance", func(s *DeploymentSpec) { s.Workloads[0].InstanceName = s.Workloads[1].InstanceName }},
		{"unsupported instance", func(s *DeploymentSpec) { s.Workloads[0].InstanceType = "host" }},
		{"nil workloads", func(s *DeploymentSpec) { s.Workloads = nil }},
		{"invalid version", func(s *DeploymentSpec) { s.APIVersion = "v9" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := validSpec()
			tc.edit(&s)
			if _, err := s.Canonical(); err == nil {
				t.Fatal("accepted invalid candidate")
			}
		})
	}
}

func TestDeploymentStrictJSON(t *testing.T) {
	s := validSpec()
	raw, err := s.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		string(raw) + " {}",
		`null`,
		`{"apiVersion":"v1","ApiVersion":"v2"}`,
		strings.Replace(string(raw), `"workloads":`, `"gateway":{"master":{"key":"not-a-secret"}},"workloads":`, 1),
		strings.Replace(string(raw), `"instanceName":`, `"InstanceName":`, 1),
		strings.Replace(string(raw), `"id":"alpha"`, `"id":"alpha","id":"beta"`, 1),
		strings.Replace(string(raw), `"profiles":[]`, `"profiles":null`, 1),
		strings.Replace(string(raw), `"ensureRunning":false`, `"ensureRunning":null`, 1),
		strings.Replace(string(raw), `,"ensureRunning":false`, ``, 1),
	} {
		_, err := ParseDeploymentSpec([]byte(bad))
		if err == nil {
			t.Fatalf("accepted invalid JSON: %s", bad)
		}
		if strings.Contains(err.Error(), "not-a-secret") {
			t.Fatal("error leaked credential-like content")
		}
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if len(m) != 4 {
		t.Fatal("unexpected broad configuration fields")
	}
}

func FuzzDeploymentParsing(f *testing.F) {
	s, _ := validSpec().Canonical()
	f.Add(s)
	f.Add([]byte("null"))
	f.Fuzz(func(t *testing.T, data []byte) {
		spec, err := ParseDeploymentSpec(data)
		if err != nil {
			return
		}
		b, err := spec.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		again, err := ParseDeploymentSpec(b)
		if err != nil {
			t.Fatal(err)
		}
		c, _ := again.Canonical()
		if !bytes.Equal(b, c) {
			t.Fatal("nonidempotent canonicalization")
		}
	})
}
