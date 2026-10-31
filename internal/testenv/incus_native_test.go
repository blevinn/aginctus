package testenv

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	incusclient "github.com/lxc/incus/v7/client"
)

type incusTestRoundTrip func(*http.Request) (*http.Response, error)

func (f incusTestRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func (f incusTestRoundTrip) Transport() *http.Transport                        { return &http.Transport{} }

func nativeHTTPTestClient(t *testing.T, transport http.RoundTripper) *nativeIncusVMClient {
	t.Helper()
	server, err := incusclient.ConnectIncusHTTPWithContext(context.Background(),
		&incusclient.ConnectionArgs{SkipGetServer: true, SkipGetEvents: true}, &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	return &nativeIncusVMClient{server: server.(*incusclient.ProtocolIncus)}
}

func testIncusResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestIncusNativeRequestsHonorContextAndProject(t *testing.T) {
	for _, method := range []string{"project", "image", "get", "list", "create", "start", "stop", "delete", "root-volume"} {
		t.Run(method, func(t *testing.T) {
			n := nativeHTTPTestClient(t, incusTestRoundTrip(func(r *http.Request) (*http.Response, error) {
				if r.URL.Query().Get("project") != DefaultVMProject {
					t.Error("lost project scope")
				}
				<-r.Context().Done()
				return nil, r.Context().Err()
			}))
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				var err error
				switch method {
				case "project":
					_, err = n.ProjectConfig(ctx)
				case "image":
					_, err = n.HasImage(ctx, strings.Repeat("a", 64))
				case "get":
					_, err = n.Get(ctx, "testenv-vm-test")
				case "list":
					_, err = n.List(ctx)
				case "create":
					ref := VMRef{Backend: IncusBackend, Project: DefaultVMProject, Instance: "testenv-vm-test", RunID: "vm-" + strings.Repeat("a", 24), ImageFingerprint: strings.Repeat("a", 64)}
					plan, planErr := PlanIncusVM(admittedSpec(), ref, "testenv", time.Now())
					if planErr != nil {
						done <- planErr
						return
					}
					err = n.Create(ctx, plan)
				case "start":
					err = n.Start(ctx, "testenv-vm-test")
				case "stop":
					err = n.Stop(ctx, "testenv-vm-test")
				case "delete":
					err = n.Delete(ctx, "testenv-vm-test")
				case "root-volume":
					_, err = n.RootVolumePresent(ctx, "testenv", "testenv-vm-test")
				}
				done <- err
			}()
			select {
			case err := <-done:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("lost deadline: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("native request ignored deadline")
			}
		})
	}
}

func TestIncusRootVolumeInventoryRequiresScopedIndependentResponse(t *testing.T) {
	for _, tc := range []struct {
		name, body       string
		present, wantErr bool
	}{
		{"present", `{"type":"sync","status_code":200,"metadata":{"name":"testenv-vm-test","type":"virtual-machine"}}`, true, false},
		{"absent", `{"type":"error","error_code":404,"error":"missing"}`, false, false},
		{"denied", `{"type":"error","error_code":403,"error":"synthetic-secret"}`, false, true},
		{"wrong-volume", `{"type":"sync","status_code":200,"metadata":{"name":"foreign","type":"virtual-machine"}}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := nativeHTTPTestClient(t, incusTestRoundTrip(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.URL.Path != "/1.0/storage-pools/testenv/volumes/virtual-machine/testenv-vm-test" || r.URL.Query().Get("project") != DefaultVMProject {
					t.Fatal("volume query lost scope", r.URL)
				}
				response := testIncusResponse(tc.body)
				if tc.name == "absent" {
					response.StatusCode = http.StatusNotFound
				}
				if tc.name == "denied" {
					response.StatusCode = http.StatusForbidden
				}
				return response, nil
			}))
			present, err := n.RootVolumePresent(context.Background(), "testenv", "testenv-vm-test")
			if present != tc.present || (err != nil) != tc.wantErr {
				t.Fatalf("bad inventory result: %v %v", present, err)
			}
			if err != nil && strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatal("daemon error leaked")
			}
		})
	}
}

func TestIncusRootVolumeCancellationIsNeverAbsence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := nativeHTTPTestClient(t, incusTestRoundTrip(func(*http.Request) (*http.Response, error) {
		cancel()
		response := testIncusResponse(`{"type":"error","error_code":404,"error":"missing"}`)
		response.StatusCode = http.StatusNotFound
		return response, nil
	}))
	if present, err := n.RootVolumePresent(ctx, "testenv", "testenv-vm-test"); present || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation substituted for volume absence: %v %v", present, err)
	}
}

func TestIncusNativePerCallCopyDoesNotRaceOrShareCancellation(t *testing.T) {
	entered := make(chan struct{}, 2)
	n := nativeHTTPTestClient(t, incusTestRoundTrip(func(r *http.Request) (*http.Response, error) {
		entered <- struct{}{}
		if strings.HasSuffix(r.URL.Path, "/cancel") {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return testIncusResponse(`{"type":"sync","status":"Success","status_code":200,"metadata":{"name":"ok","type":"virtual-machine"}}`), nil
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if _, err := n.Get(ctx, "cancel"); !errors.Is(err, context.Canceled) {
			t.Errorf("lost cancellation: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if _, err := n.Get(context.Background(), "ok"); err != nil {
			t.Errorf("other request canceled: %v", err)
		}
	}()
	<-entered
	<-entered
	cancel()
	wg.Wait()
	if _, err := n.Get(context.Background(), "ok"); err != nil {
		t.Fatal("shared base context mutated", err)
	}
}

func TestIncusNativeOperationWaitAndErrorsAreBoundedAndRedacted(t *testing.T) {
	n := nativeHTTPTestClient(t, incusTestRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			return testIncusResponse(`{"type":"async","status":"Operation created","status_code":100,"metadata":{"id":"test-op","status_code":103,"status":"Running"}}`), nil
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := n.Start(ctx, "testenv-vm-test"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("operation wait lost deadline: %v", err)
	}
	n = nativeHTTPTestClient(t, incusTestRoundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("synthetic-secret") }))
	if _, err := n.Get(context.Background(), "testenv-vm-test"); err == nil || strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatalf("unredacted native error: %v", err)
	}
}

func TestIncusNativeOperationPollingRequiresFinalSuccess(t *testing.T) {
	for _, final := range []string{"Success", "Failure"} {
		t.Run(final, func(t *testing.T) {
			var polls atomic.Int32
			n := nativeHTTPTestClient(t, incusTestRoundTrip(func(r *http.Request) (*http.Response, error) {
				if strings.Contains(r.URL.Path, "/events") || strings.Contains(r.URL.Path, "/wait") {
					t.Error("unsafe SDK event/wait path")
				}
				if r.Method == http.MethodPut || polls.Add(1) == 1 {
					return testIncusResponse(`{"type":"async","status_code":100,"metadata":{"id":"test-op","status_code":103}}`), nil
				}
				code := "200"
				if final == "Failure" {
					code = "400"
				}
				return testIncusResponse(`{"type":"sync","status_code":200,"metadata":{"id":"test-op","status_code":` + code + `}}`), nil
			}))
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := n.Start(ctx, "testenv-vm-test")
			if polls.Load() < 2 || (err == nil) != (final == "Success") {
				t.Fatalf("unverified operation result, polls=%d: %v", polls.Load(), err)
			}
		})
	}
}
