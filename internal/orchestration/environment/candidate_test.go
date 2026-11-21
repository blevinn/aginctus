package environment

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
)

func candidateFixture() Candidate {
	return Candidate{
		APIVersion: CandidateAPIVersion,
		Kind: CandidateKind,
		Environment: Descriptor{
			ID: "7c82e6d1-1212-4123-8123-0123456789ab",
			Name: "development",
			Binding: Binding{ServerID: "verified-incus-server", Project: "dev_team"},
		},
		SelectedWorkloads: []string{"beta", "alpha"},
		Workloads: []acceptedstate.WorkloadSpec{
			{ID: "beta", InstanceName: "beta-instance", InstanceType: "container", ImageFingerprint: strings.Repeat("b", 64), Profiles: []string{"base"}, EnsureRunning: true},
			{ID: "alpha", InstanceName: "alpha-instance", InstanceType: "container", ImageFingerprint: strings.Repeat("a", 64), Profiles: []string{}, EnsureRunning: false},
		},
	}
}

func TestCandidateCanonicalRoundTripAndNoMutation(t *testing.T) {
	c := candidateFixture()
	data, err := c.Canonical()
	if err != nil { t.Fatal(err) }
	if c.SelectedWorkloads[0] != "beta" || c.Workloads[0].ID != "beta" {
		t.Fatal("canonicalization mutated candidate source")
	}
	parsed, err := ParseCandidate(data)
	if err != nil { t.Fatal(err) }
	canonical, err := parsed.Canonical()
	if err != nil || !bytes.Equal(data, canonical) { t.Fatalf("candidate unstable: %v", err) }
	if !bytes.Contains(data, []byte(`"selectedWorkloads":["alpha","beta"]`)) {
		t.Fatal("selection not canonicalized")
	}
}

func TestCandidateRejectsUnknownDuplicatedAndPrivilegedFields(t *testing.T) {
	c := candidateFixture()
	data, err := c.Canonical()
	if err != nil { t.Fatal(err) }
	for _, invalid := range []string{
		"null", string(data) + " {}",
		strings.Replace(string(data), `"kind":`, `"Kind":`, 1),
		strings.Replace(string(data), `"name":"development"`, `"name":"development","Name":"escape"`, 1),
		strings.Replace(string(data), `"serverId":`, `"serverID":`, 1),
		strings.Replace(string(data), `"workloads":`, `"gateway":{"master":{"key":"private-secret"}},"workloads":`, 1),
		strings.Replace(string(data), `"profiles":`, `"devices":{"disk":"host"},"profiles":`, 1),
		strings.Replace(string(data), `"selectedWorkloads":`, `"selectedWorkloads":null,"selectedWorkloads":`, 1),
	} {
		_, err := ParseCandidate([]byte(invalid))
		if err == nil { t.Fatalf("accepted invalid candidate: %s", invalid) }
		if strings.Contains(err.Error(), "private-secret") { t.Fatal("candidate error leaked secret") }
	}
}

func TestCandidateRejectsCrossEnvironmentAndProjectBinding(t *testing.T) {
	c := candidateFixture()
	enrolled := c.Environment
	observed := enrolled.Binding
	if _, err := SelectCandidate(c, enrolled, observed); err != nil { t.Fatal(err) }
	other := c
	other.Environment.ID = "7c82e6d1-1212-4123-8123-0123456789ac"
	if _, err := SelectCandidate(other, enrolled, observed); err == nil {
		t.Fatal("distinct environment inside same project accepted")
	}
	other = c
	other.Environment.Binding.Project = "other"
	if _, err := SelectCandidate(other, enrolled, observed); err == nil {
		t.Fatal("candidate silently changed project")
	}
	observed.Project = "other"
	if _, err := SelectCandidate(c, enrolled, observed); err == nil {
		t.Fatal("observed Incus project redirect accepted")
	}
}

func TestCandidateNoImplicitFileOrSettingsMerge(t *testing.T) {
	c := candidateFixture()
	data, err := json.Marshal(c)
	if err != nil { t.Fatal(err) }
	path := filepath.Join(t.TempDir(), "candidate.json")
	if err := os.WriteFile(path, data, 0o600); err != nil { t.Fatal(err) }
	if _, err := ReadCandidateFile(""); err == nil { t.Fatal("implicit source accepted") }
	if _, err := ReadCandidateFile("candidate.json"); err == nil { t.Fatal("relative candidate source accepted") }
	t.Setenv("AGINCTUS_INCUS_PROJECT", "other")
	selected, err := ReadCandidateFile(path)
	if err != nil { t.Fatal(err) }
	if selected.Environment.Binding.Project != "dev_team" {
		t.Fatal("generic environment configuration overrode candidate")
	}
	if _, err := ReadCandidateFile(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("missing selected candidate silently fell back")
	}
}

func TestCandidateRejectsSpecialAndOversizedFiles(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	// A blocking Open would hang this test indefinitely.
	if _, err := ReadCandidateFile(fifo); err == nil {
		t.Fatal("accepted FIFO as candidate")
	}
	if _, err := ReadCandidateFile(dir); err == nil {
		t.Fatal("accepted directory as candidate")
	}
	oversized := filepath.Join(dir, "oversized.json")
	f, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxCandidateBytes + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCandidateFile(oversized); err == nil {
		t.Fatal("accepted oversized candidate")
	}
	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCandidateFile(empty); err == nil {
		t.Fatal("accepted empty candidate")
	}
}

func TestCandidateReadsExplicitSymlinkToRegularFile(t *testing.T) {
	dir := t.TempDir()
	payload, err := candidateFixture().Canonical()
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "candidate.json")
	if err := os.WriteFile(target, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "selected.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	c, err := ReadCandidateFile(link)
	if err != nil {
		t.Fatal(err)
	}
	if c.Environment.ID != candidateFixture().Environment.ID {
		t.Fatal("candidate identity changed through explicit file link")
	}
}
