package native

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

const privateObservationValue = "private-fixture-value"

func TestObservePinnedAcceptedWorkloadsSDK(t *testing.T) {
	for _, tc := range []struct {
		name                                     string
		missingStatus                            int
		plain404, foreign, grantDefaultAfterRead bool
		retarget, cancelAfterRead, editInput     bool
	}{
		{name: "selected drift and typed absence", missingStatus: http.StatusNotFound},
		{name: "denial", missingStatus: http.StatusForbidden},
		{name: "uncertain", missingStatus: http.StatusServiceUnavailable},
		{name: "non-Incus 404", missingStatus: http.StatusNotFound, plain404: true},
		{name: "foreign environment", missingStatus: http.StatusNotFound, foreign: true},
		{name: "confinement changes", missingStatus: http.StatusNotFound, grantDefaultAfterRead: true},
		{name: "transport retargeted between reads", retarget: true},
		{name: "canceled between reads", cancelAfterRead: true},
		{name: "caller edit cannot retarget the pin", missingStatus: http.StatusNotFound, editInput: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate, _, observations := acceptedAssessmentFixture(t)
			worker := observations[0].Instance
			worker.Config["environment.PRIVATE"] = privateObservationValue
			if tc.foreign {
				worker.Config[environmentOwnershipKey] = "2e55a86f-4ee8-41ce-834c-786ab8c9dffd"
			}
			instance := api.Instance{
				InstancePut: api.InstancePut{Config: worker.Config, Profiles: []string{"changed-private-profile"}},
				Name:        worker.Identity.Name, Project: worker.Identity.Project,
				Type: string(api.InstanceTypeContainer), Status: "Stopped",
			}
			var mu sync.Mutex
			var reads []string
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Method != http.MethodGet {
					t.Errorf("unexpected method %s", r.Method)
					http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
					return
				}
				status := http.StatusOK
				var metadata any
				switch r.URL.Path {
				case "/1.0":
					metadata = api.Server{ServerUntrusted: api.ServerUntrusted{Auth: "trusted", APIExtensions: []string{"projects"}}}
				case "/1.0/projects/team":
					metadata = api.Project{Name: "team"}
				case "/1.0/projects/default":
					if tc.grantDefaultAfterRead && len(reads) > 0 {
						metadata = api.Project{Name: "default"}
					} else {
						status = http.StatusForbidden
					}
				case "/1.0/instances/worker-1", "/1.0/instances/missing-1":
					if r.URL.RawQuery != "project=team" {
						t.Errorf("instance request used unexpected query: %s", r.URL.RawQuery)
					}
					reads = append(reads, r.URL.Path)
					if r.URL.Path == "/1.0/instances/worker-1" {
						metadata = instance
					} else {
						if tc.plain404 {
							http.Error(w, privateObservationValue, http.StatusNotFound)
							return
						}
						status = tc.missingStatus
					}
				default:
					t.Errorf("unexpected SDK request %s", r.URL)
					status = http.StatusBadRequest
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("ETag", `"fixture-etag"`)
				w.WriteHeader(status)
				response := map[string]any{"type": "sync", "status_code": 200, "metadata": metadata}
				if status != http.StatusOK {
					response = map[string]any{"type": "error", "error_code": status, "error": privateObservationValue}
				}
				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Error(err)
				}
			}))
			defer srv.Close()
			cert := srv.Certificate().Raw
			pin := fmt.Sprintf("%x", sha256.Sum256(cert))
			candidate.Environment.Binding.ServerID = "sha256:" + pin
			missing := candidate.Workloads[0]
			missing.ID, missing.InstanceName = "missing", "missing-1"
			candidate.SelectedWorkloads = append(candidate.SelectedWorkloads, missing.ID)
			candidate.Workloads = append(candidate.Workloads, missing)
			revision, err := environment.BuildAcceptedRevision(nil, nil, candidate)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			sdk, err := incusclient.ConnectIncusWithContext(ctx, srv.URL, &incusclient.ConnectionArgs{
				TLSServerCert: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert})),
				SkipGetEvents: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer sdk.Disconnect()
			scoped := sdk.UseProject("team")
			var reader AcceptedWorkloadIncusReader = scoped
			if tc.retarget || tc.cancelAfterRead || tc.editInput {
				retargeted := false
				reader = acceptedIncusTestReader{
					AcceptedWorkloadIncusReader: scoped,
					info: func() (*incusclient.ConnectionInfo, error) {
						info, err := scoped.GetConnectionInfo()
						if retargeted {
							info.Target = "other-member"
						}
						return info, err
					},
					read: func(name string) (*api.Instance, string, error) {
						instance, etag, err := scoped.GetInstance(name)
						retargeted = tc.retarget
						if tc.cancelAfterRead {
							cancel()
						}
						if tc.editInput {
							candidate.Workloads[1].InstanceName = "unselected-1"
							candidate.Workloads[0].Profiles[0] = "changed-private-profile"
						}
						return instance, etag, err
					},
				}
			}
			got, err := ObservePinnedAcceptedWorkloads(ctx, candidate.Environment, revision, candidate, reader, pin)
			mu.Lock()
			defer mu.Unlock()
			if tc.retarget || tc.cancelAfterRead {
				assertNoAcceptedObservation(t, got, err)
				if !slices.Equal(reads, []string{"/1.0/instances/worker-1"}) {
					t.Fatalf("reads continued after cancellation or scope change: %v", reads)
				}
				if tc.cancelAfterRead && !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation lost: %v", err)
				}
				return
			}
			if !slices.Equal(reads, []string{"/1.0/instances/worker-1", "/1.0/instances/missing-1"}) {
				t.Fatalf("instance reads: %v (observation error: %v)", reads, err)
			}
			if tc.missingStatus != http.StatusNotFound || tc.plain404 || tc.foreign || tc.grantDefaultAfterRead {
				assertNoAcceptedObservation(t, got, err)
				return
			}
			want := AcceptedEnvironmentAssessment{
				EnvironmentID: candidate.Environment.ID, ServerID: revision.ServerID,
				Project: revision.Project, RevisionID: revision.ID, Generation: revision.Generation,
				Workloads: []AcceptedWorkloadAssessment{
					{WorkloadID: "missing", InstanceName: "missing-1", Status: "absent", Fields: []string{}},
					{WorkloadID: "worker", InstanceName: "worker-1", Status: "drift", Fields: []string{"profiles", "ensureRunning"}},
				},
			}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("assessment = %+v, %v; want %+v", got, err, want)
			}
		})
	}
}

type acceptedIncusTestReader struct {
	AcceptedWorkloadIncusReader
	info func() (*incusclient.ConnectionInfo, error)
	read func(string) (*api.Instance, string, error)
}

func (r acceptedIncusTestReader) GetConnectionInfo() (*incusclient.ConnectionInfo, error) {
	return r.info()
}
func (r acceptedIncusTestReader) GetInstance(name string) (*api.Instance, string, error) {
	return r.read(name)
}

func TestObservePinnedAcceptedWorkloadsRejectsInputsBeforeEffects(t *testing.T) {
	for _, name := range []string{"stale input", "bad pin", "other pin", "wrong enrollment", "nil context", "canceled"} {
		t.Run(name, func(t *testing.T) {
			candidate, _, _ := acceptedAssessmentFixture(t)
			pin := strings.Repeat("a", 64)
			candidate.Environment.Binding.ServerID = "sha256:" + pin
			revision, err := environment.BuildAcceptedRevision(nil, nil, candidate)
			if err != nil {
				t.Fatal(err)
			}
			enrolled := candidate.Environment
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch name {
			case "stale input":
				candidate.Workloads[0].InstanceName = "different-valid-name"
			case "bad pin":
				pin = privateObservationValue
			case "other pin":
				pin = strings.Repeat("b", 64)
			case "wrong enrollment":
				enrolled.ID = "7c82e6d1-1212-4123-8123-0123456789ac"
			case "nil context":
				ctx = nil
			case "canceled":
				cancel()
			}
			reader := acceptedIncusTestReader{info: func() (*incusclient.ConnectionInfo, error) {
				t.Fatal("input validation reached reader")
				return nil, nil
			}}
			got, err := ObservePinnedAcceptedWorkloads(ctx, enrolled, revision, candidate, reader, pin)
			assertNoAcceptedObservation(t, got, err)
			if name == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
		})
	}
}

func assertNoAcceptedObservation(t *testing.T, got AcceptedEnvironmentAssessment, err error) {
	t.Helper()
	if err == nil || !reflect.DeepEqual(got, AcceptedEnvironmentAssessment{}) || strings.Contains(err.Error(), privateObservationValue) {
		t.Fatalf("wanted redacted error and no assessment: %+v, %v", got, err)
	}
}
