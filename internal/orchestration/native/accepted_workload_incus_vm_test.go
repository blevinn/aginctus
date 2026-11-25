package native

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
	"github.com/lxc/incus/v7/shared/api"
)

func TestObservePinnedAcceptedWorkloadsVM(t *testing.T) {
	if os.Getenv("AGINCTUS_ACCEPTED_OBSERVATION_VM") != "disposable-vm" {
		t.Skip("run via the disposable accepted observation NixOS VM target")
	}
	if marker, err := os.Lstat("/run/aginctus-accepted-observation-disposable"); err != nil ||
		!marker.Mode().IsRegular() || marker.Mode().Perm()&0o022 != 0 || os.Geteuid() == 0 {
		t.Fatal("observation requires the disposable guest marker and a non-root principal")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	const project = "team"
	serverPin := os.Getenv("AGINCTUS_OBSERVATION_SERVER_PIN")
	image := os.Getenv("AGINCTUS_OBSERVATION_IMAGE")
	scoped, err := environment.OpenExplicitRestrictedIncusProject(ctx, environment.ExplicitRestrictedIncusConnection{
		URL: "https://127.0.0.1:8443", Project: project,
		ServerCertificatePEM: read("/run/aginctus-observation-server.crt"), TrustedServerSHA256: serverPin,
		ClientCertificatePEM: read("/home/operator/.config/incus/client.crt"),
		ClientPrivateKeyPEM:   read("/home/operator/.config/incus/client.key"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer scoped.Disconnect()

	enrolled := environment.Descriptor{
		ID:      "7c82e6d1-1212-4123-8123-0123456789ab",
		Name:    "development",
		Binding: environment.Binding{ServerID: "sha256:" + serverPin, Project: project},
	}
	workload := func(id, name string, ensureRunning bool) acceptedstate.WorkloadSpec {
		return acceptedstate.WorkloadSpec{
			ID: id, InstanceName: name, InstanceType: "container", ImageFingerprint: image,
			Profiles: []string{"default"}, EnsureRunning: ensureRunning,
		}
	}
	// Commit only to a same-UID private fixture, then observe its immutable
	// snapshot while holding the pin. This does not authenticate accepted state.
	pinCandidate := func(t *testing.T, specs ...acceptedstate.WorkloadSpec) (environment.AcceptedRevision, environment.Candidate) {
		t.Helper()
		candidate := environment.Candidate{
			APIVersion: environment.CandidateAPIVersion, Kind: environment.CandidateKind,
			Environment: enrolled, Workloads: specs,
		}
		for _, spec := range specs {
			candidate.SelectedWorkloads = append(candidate.SelectedWorkloads, spec.ID)
		}
		store, err := environment.InitializeLocalProjectStateStore(filepath.Join(t.TempDir(), "state"), enrolled.Binding)
		if err != nil {
			t.Fatal(err)
		}
		rev, err := store.CommitLocalCandidate(ctx, enrolled, enrolled.Binding, nil, candidate)
		if err != nil {
			t.Fatal(err)
		}
		pin, err := store.PinLocalAccepted(ctx, enrolled, rev, specs[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			defer func() {
				if err := pin.Close(); err != nil {
					t.Error(err)
				}
			}()
			if err := pin.Check(ctx); err != nil {
				t.Errorf("fixture pin invalid after observation: %v", err)
			}
		})
		snapshot, err := pin.SnapshotCandidate(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return rev, snapshot
	}

	t.Run("owned-drift-and-permitted-absence", func(t *testing.T) {
		rev, candidate := pinCandidate(t, workload("worker", "worker-1", false), workload("drift", "drift-1", true), workload("missing", "missing-1", false))
		got, err := ObservePinnedAcceptedWorkloads(ctx, enrolled, rev, candidate, scoped, serverPin)
		want := AcceptedEnvironmentAssessment{
			EnvironmentID: enrolled.ID, ServerID: rev.ServerID, Project: project, RevisionID: rev.ID, Generation: rev.Generation,
			Workloads: []AcceptedWorkloadAssessment{
				{WorkloadID: "drift", InstanceName: "drift-1", Status: "drift", Fields: []string{"ensureRunning"}},
				{WorkloadID: "missing", InstanceName: "missing-1", Status: "absent", Fields: []string{}},
				{WorkloadID: "worker", InstanceName: "worker-1", Status: "converged", Fields: []string{}},
			},
		}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("assessment = %+v, %v; want %+v", got, err, want)
		}
	})

	t.Run("wrong-scope-denial-is-not-absence", func(t *testing.T) {
		rev, candidate := pinCandidate(t, workload("missing", "missing-1", false))
		wrong := scoped.UseProject("unrelated")
		// Same TLS principal and name. The allowed project returned absence;
		// for the populated foreign project the real server must return denial.
		if instance, _, err := wrong.GetInstance("missing-1"); instance != nil || !api.StatusErrorCheck(err, http.StatusForbidden) {
			t.Fatalf("foreign project named read: want Incus HTTP 403, got %v", err)
		}
		got, err := ObservePinnedAcceptedWorkloads(ctx, enrolled, rev, candidate, wrong, serverPin)
		assertNoAcceptedObservation(t, got, err)
		if err.Error() != "accepted workload Incus project verification failed" {
			t.Fatalf("wrong-scope observation failed for unexpected reason: %v", err)
		}
	})

	t.Run("readable-same-project-collision-is-not-owned", func(t *testing.T) {
		rev, candidate := pinCandidate(t, workload("worker", "foreign-1", false))
		instance, etag, err := scoped.GetInstance("foreign-1")
		if err != nil || instance == nil || etag == "" || instance.Project != project ||
			instance.Config[environmentOwnershipKey] != "7c82e6d1-1212-4123-8123-0123456789ac" {
			t.Fatalf("expected a readable same-project peer workload: %v", err)
		}
		got, err := ObservePinnedAcceptedWorkloads(ctx, enrolled, rev, candidate, scoped, serverPin)
		assertNoAcceptedObservation(t, got, err)
		if err.Error() != "foreign or unowned observed environment workload" {
			t.Fatalf("collision failed for unexpected reason: %v", err)
		}
	})
}
