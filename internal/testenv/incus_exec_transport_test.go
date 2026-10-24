package testenv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lxc/incus/v7/shared/api"
	"github.com/lxc/incus/v7/shared/ws"
)

type incusExecMock struct {
	mode     string
	active   atomic.Int32
	canceled atomic.Int32
	requests chan api.InstanceExecPost
}

func nativeExecMock(t *testing.T, mode string) (*nativeIncusVMClient, *incusExecMock) {
	t.Helper()
	m := &incusExecMock{mode: mode, requests: make(chan api.InstanceExecPost, 1)}
	mock := httptest.NewTLSServer(http.HandlerFunc(m.serve))
	client := mock.Client()
	transport := client.Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.InsecureSkipVerify = true // synthetic local server, no worker credentials
	transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", mock.Listener.Addr().String())
	}
	t.Cleanup(func() { transport.CloseIdleConnections(); mock.CloseClientConnections(); mock.Close() })
	return nativeHTTPTestClient(t, transport), m
}

func (m *incusExecMock) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("project") != DefaultVMProject {
		http.Error(w, "project scope missing", 400)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/websocket") {
		if m.mode == "handshake-timeout" {
			<-r.Context().Done()
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		m.active.Add(1)
		defer m.active.Add(-1)
		defer conn.Close()
		secret := r.URL.Query().Get("secret")
		if secret == "0" || secret == "control" {
			_, _ = io.Copy(io.Discard, ws.NewWrapper(conn))
			return
		}
		if m.mode == "stream-timeout" {
			_, _ = io.Copy(io.Discard, ws.NewWrapper(conn))
			return
		}
		text := "stdout"
		if secret == "2" {
			text = "stderr"
		}
		if m.mode == "overflow" {
			text = strings.Repeat("x", MaxGuestOutput+1)
		}
		if m.mode == "exact-limit" && secret == "1" {
			text = strings.Repeat("x", MaxGuestOutput-len("stderr"))
		}
		_ = conn.WriteMessage(websocket.BinaryMessage, []byte(text))
		if m.mode == "broken-stdout" && secret == "1" || m.mode == "broken-stderr" && secret == "2" {
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, nil) // Incus stream completion barrier
		_, _ = io.Copy(io.Discard, ws.NewWrapper(conn))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodPost {
		var request api.InstanceExecPost
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(w, "bad request", 400)
			return
		}
		m.requests <- request
		fds := map[string]string{"0": "0", "1": "1", "2": "2", api.SecretNameControl: "control"}
		if m.mode == "missing-fd" {
			delete(fds, "2")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "async", "status_code": 100, "metadata": map[string]any{"id": "probe", "status_code": 103, "metadata": map[string]any{"fds": fds}}})
		return
	}
	if r.Method == http.MethodDelete {
		m.canceled.Add(1)
		if m.mode == "cancel-timeout" {
			<-r.Context().Done()
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "sync", "status_code": 200, "metadata": map[string]any{}})
		return
	}
	if m.mode == "poll-timeout" {
		<-r.Context().Done()
		return
	}
	status := 200
	if m.mode == "running-timeout" {
		status = 103
	}
	metadata := map[string]any{"return": 17}
	if m.mode == "missing-exit" {
		delete(metadata, "return")
	}
	if m.mode == "cancel-timeout" {
		metadata["return"] = -1
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "sync", "status_code": 200, "metadata": map[string]any{"id": "probe", "status_code": status, "metadata": metadata}})
}

func TestIncusNativeExecVerifiesStreamsAndGuestExit(t *testing.T) {
	n, mock := nativeExecMock(t, "success")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	argv := []string{"/bin/echo", "x;touch /tmp/never", "$(id)"}
	out, err := n.ExecGuest(ctx, "testenv-vm-probe", argv)
	if err != nil || out.ExitCode != 17 || out.Stdout != "stdout" || out.Stderr != "stderr" {
		t.Fatalf("lost verified guest result: %+v %v", out, err)
	}
	request := <-mock.requests
	if !reflect.DeepEqual(request.Command, argv) || !request.WaitForWS || request.Interactive || request.RecordOutput || request.Environment["LANG"] != "C" {
		t.Fatalf("unsafe request: %+v", request)
	}
	waitExecSocketsClosed(t, mock)
}

func TestIncusNativeExecAllowsExactCombinedOutputLimit(t *testing.T) {
	n, m := nativeExecMock(t, "exact-limit")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := n.ExecGuest(ctx, "testenv-vm-probe", []string{"/bin/true"})
	if err != nil || len(out.Stdout)+len(out.Stderr) != MaxGuestOutput || out.ExitCode != 17 {
		t.Fatalf("exact bound failed: bytes=%d err=%v", len(out.Stdout)+len(out.Stderr), err)
	}
	waitExecSocketsClosed(t, m)
}

func TestIncusNativeExecCancellationClosesStreams(t *testing.T) {
	n, m := nativeExecMock(t, "stream-timeout")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := n.ExecGuest(ctx, "testenv-vm-probe", []string{"/bin/true"}); done <- err }()
	select {
	case <-m.requests:
	case <-time.After(time.Second):
		t.Fatal("exec not submitted")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("lost cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("exec did not cancel")
	}
	waitExecSocketsClosed(t, m)
}

func waitExecSocketsClosed(t *testing.T, m *incusExecMock) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for m.active.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if m.active.Load() != 0 {
		t.Fatalf("%d exec sockets leaked", m.active.Load())
	}
}

func TestIncusNativeExecRejectsBrokenOrUnverifiedTransport(t *testing.T) {
	for _, mode := range []string{"broken-stdout", "broken-stderr", "missing-fd", "missing-exit", "overflow", "cancel-timeout"} {
		t.Run(mode, func(t *testing.T) {
			n, m := nativeExecMock(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			out, err := n.ExecGuest(ctx, "testenv-vm-probe", []string{"/bin/true"})
			if err == nil || out != (VMExecResult{}) {
				t.Fatalf("unverified success: %+v %v", out, err)
			}
			if mode == "overflow" && !errors.Is(err, ErrGuestOutputLimit) {
				t.Fatalf("lost output classification: %v", err)
			}
			waitExecSocketsClosed(t, m)
			if m.canceled.Load() == 0 {
				t.Fatal("no best-effort cancellation attempted")
			}
		})
	}
}

func TestIncusNativeExecDeadlineCoversSetupPollingAndStreamClose(t *testing.T) {
	for _, mode := range []string{"handshake-timeout", "poll-timeout", "running-timeout", "stream-timeout"} {
		t.Run(mode, func(t *testing.T) {
			n, m := nativeExecMock(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := n.ExecGuest(ctx, "testenv-vm-probe", []string{"/bin/true"}); done <- err }()
			select {
			case err := <-done:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("lost deadline: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("execution ignored deadline")
			}
			waitExecSocketsClosed(t, m)
		})
	}
}

func TestIncusNativeExecSubmissionHonorsDeadlineAndRedactsError(t *testing.T) {
	n := nativeHTTPTestClient(t, incusTestRoundTrip(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, fmt.Errorf("synthetic-secret: %w", r.Context().Err())
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := n.ExecGuest(ctx, "testenv-vm-probe", []string{"/bin/true"})
	if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatalf("unsafe submission result: %v", err)
	}
}

type deadlineGuestClient struct {
	*faultIncusClient
	preflightDeadline  time.Time
	execDeadline       time.Time
	blockPreflight     bool
	ignoreCancellation bool
}

func (c *deadlineGuestClient) ProjectConfig(ctx context.Context) (map[string]string, error) {
	c.preflightDeadline, _ = ctx.Deadline()
	if c.blockPreflight {
		<-ctx.Done()
		return nil, fmt.Errorf("synthetic-secret: %w", ctx.Err())
	}
	return c.faultIncusClient.ProjectConfig(ctx)
}
func (c *deadlineGuestClient) ExecGuest(ctx context.Context, _ string, _ []string) (VMExecResult, error) {
	c.execDeadline, _ = ctx.Deadline()
	if c.ignoreCancellation {
		<-ctx.Done()
		return VMExecResult{Stdout: "unverified"}, nil
	}
	return VMExecResult{}, nil
}

func TestIncusExecDeadlineIncludesPreflightAndRejectsLateSuccess(t *testing.T) {
	for _, phase := range []string{"preflight", "same-deadline", "late-success"} {
		t.Run(phase, func(t *testing.T) {
			c := &deadlineGuestClient{faultIncusClient: &faultIncusClient{fakeIncusVMClient: newFakeIncusClient(t)}}
			b, _ := newTestIncusBackend(c, "testenv")
			ref, err := b.Create(context.Background(), admittedSpec())
			if err != nil {
				t.Fatal(err)
			}
			c.blockPreflight = phase == "preflight"
			c.ignoreCancellation = phase == "late-success"
			out, err := b.Exec(context.Background(), ref, []string{"/bin/true"}, 20*time.Millisecond)
			if phase == "same-deadline" {
				if err != nil || c.execDeadline.IsZero() || !c.execDeadline.Equal(c.preflightDeadline) {
					t.Fatalf("preflight not in execution deadline: %v", err)
				}
			} else if !errors.Is(err, context.DeadlineExceeded) || out != (VMExecResult{}) {
				t.Fatalf("unverified deadline result: %+v %v", out, err)
			}
			if err != nil && strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatal("preflight leaked error")
			}
		})
	}
}

func TestIncusExecPreflightErrorsPreserveSafeClassifications(t *testing.T) {
	for _, phase := range []string{"project", "get"} {
		for _, kind := range []error{context.Canceled, context.DeadlineExceeded, ErrIncusInstanceMissing} {
			t.Run(phase+kind.Error(), func(t *testing.T) {
				c := &deadlineGuestClient{faultIncusClient: &faultIncusClient{fakeIncusVMClient: newFakeIncusClient(t)}}
				b, _ := newTestIncusBackend(c, "testenv")
				ref, err := b.Create(context.Background(), admittedSpec())
				if err != nil {
					t.Fatal(err)
				}
				c.fault = phase
				c.err = fmt.Errorf("synthetic-secret: %w", kind)
				_, err = b.Exec(context.Background(), ref, []string{"/bin/true"}, time.Second)
				if !errors.Is(err, kind) || strings.Contains(err.Error(), "synthetic-secret") || !c.execDeadline.IsZero() {
					t.Fatalf("unsafe preflight result: %v", err)
				}
			})
		}
	}
}

func TestIncusExecRejectsExtendedMutableLifetime(t *testing.T) {
	c := &fakeGuestIncusClient{fakeIncusVMClient: newFakeIncusClient(t)}
	b, _ := newTestIncusBackend(c, "testenv")
	now := time.Now()
	b.now = func() time.Time { return now }
	ref, err := b.Create(context.Background(), admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	v := c.vms[ref.Instance]
	v.Config["user.aginctus.testenv.expires"] = now.Add(24 * time.Hour).Format(time.RFC3339Nano)
	c.vms[ref.Instance] = v
	b.now = func() time.Time { return now.Add(3 * time.Hour) }
	if _, err := b.Exec(context.Background(), ref, []string{"/bin/true"}, time.Second); !errors.Is(err, ErrOwnershipMismatch) || len(c.argv) != 0 {
		t.Fatalf("mutable expiry admitted guest exec: %v", err)
	}
}
