package testenv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
	"github.com/lxc/incus/v7/shared/ws"
)

// guestOutputBuffer drops excess output rather than blocking the Incus
// websocket readers. Overflow is surfaced as a failed exec, never as
// silently truncated successful guest output.
type guestOutputBuffer struct {
	mu       sync.Mutex
	stdout   bytes.Buffer
	stderr   bytes.Buffer
	total    int
	overflow bool
}

type guestStreamWriter struct {
	buffer         *guestOutputBuffer
	stdout         bool
	failOnOverflow bool
}

func (w guestStreamWriter) Write(p []byte) (int, error) {
	w.buffer.mu.Lock()
	defer w.buffer.mu.Unlock()
	available := MaxGuestOutput - w.buffer.total
	if available < 0 {
		available = 0
	}
	accepted := len(p)
	if accepted > available {
		accepted = available
		w.buffer.overflow = true
	}
	if w.stdout {
		_, _ = w.buffer.stdout.Write(p[:accepted])
	} else {
		_, _ = w.buffer.stderr.Write(p[:accepted])
	}
	w.buffer.total += accepted
	if w.failOnOverflow && w.buffer.overflow {
		return accepted, ErrGuestOutputLimit
	}
	// Report the whole frame consumed so streaming is not blocked.
	return len(p), nil
}

func (b *guestOutputBuffer) Result(exitCode int) (VMExecResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.overflow {
		return VMExecResult{}, ErrGuestOutputLimit
	}
	return VMExecResult{
		ExitCode: exitCode,
		Stdout:   b.stdout.String(),
		Stderr:   b.stderr.String(),
	}, nil
}

// ExecGuest uses the native Incus VM agent; no external incus CLI, host
// shell or guest arbitrary host path. All client options are fixed here.
func (n *nativeIncusVMClient) ExecGuest(ctx context.Context, instance string, argv []string) (VMExecResult, error) {
	if err := ctx.Err(); err != nil {
		return VMExecResult{}, err
	}
	ctx, server, cancel := n.requestClient(ctx)
	defer cancel()
	output := &guestOutputBuffer{}
	exec := api.InstanceExecPost{
		Command: argv, WaitForWS: true, Interactive: false,
		RecordOutput: false,
		Environment:  map[string]string{"PATH": "/run/current-system/sw/bin:/usr/bin:/bin", "LANG": "C"},
	}
	// RawQuery avoids ExecInstance's discarded mirror errors and unbounded
	// websocket setup. Explicit project scoping is required by this SDK API.
	path := "/1.0/instances/" + url.PathEscape(instance) + "/exec?project=" + url.QueryEscape(DefaultVMProject)
	response, _, err := server.RawQuery(http.MethodPost, path, exec, "")
	if err != nil {
		return VMExecResult{}, guestExecError(ctx, "testenv Incus guest agent execution unavailable", err)
	}
	op, err := response.MetadataAsOperation()
	if err != nil || response.Type != "async" || op == nil || op.ID == "" || len(op.ID) > 128 {
		return VMExecResult{}, errors.New("testenv Incus guest operation response invalid")
	}
	var wg sync.WaitGroup
	var closeSockets []func()
	verified := false
	defer func() {
		// Cancellation is best effort, inside the original execution deadline.
		if !verified {
			n.cancelGuestOperation(ctx, op.ID)
		}
		cancel()
		for _, closeSocket := range closeSockets {
			closeSocket()
		}
		wg.Wait()
	}()
	fds, ok := op.Metadata["fds"].(map[string]any)
	if !ok {
		return VMExecResult{}, errors.New("testenv Incus guest stream descriptors missing")
	}
	connections := make(map[string]*websocket.Conn)
	for _, fd := range []string{api.SecretNameControl, "0", "1", "2"} {
		secret, valid := fds[fd].(string)
		if fd == api.SecretNameControl && !valid {
			continue
		}
		if !valid || secret == "" || len(secret) > 512 {
			return VMExecResult{}, errors.New("testenv Incus guest stream descriptor invalid")
		}
		conn, err := dialIncusExecSocket(ctx, server, op.ID, secret)
		if err != nil {
			return VMExecResult{}, guestExecError(ctx, "testenv Incus guest stream setup failed", err)
		}
		connections[fd] = conn
		stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
		closeSockets = append(closeSockets, func() { stopClose(); _ = conn.Close() })
	}
	if err := ws.NewWrapper(connections["0"]).Close(); err != nil {
		return VMExecResult{}, guestExecError(ctx, "testenv Incus guest stdin completion failed", err)
	}
	if control := connections[api.SecretNameControl]; control != nil {
		control.SetReadLimit(4096)
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = io.Copy(io.Discard, ws.NewWrapper(control)) }()
	}
	streams := make(chan error, 2)
	for _, fd := range []string{"1", "2"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := io.Copy(guestStreamWriter{buffer: output, stdout: fd == "1", failOnOverflow: true}, ws.NewWrapper(connections[fd]))
			streams <- err
		}()
	}
	type completion struct {
		operation *api.Operation
		err       error
	}
	completed := make(chan completion, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		operation, err := pollIncusExec(ctx, server, op.ID)
		completed <- completion{operation, err}
	}()
	var final *api.Operation
	for done := 0; done < 3; done++ {
		select {
		case err := <-streams:
			if errors.Is(err, ErrGuestOutputLimit) {
				return VMExecResult{}, ErrGuestOutputLimit
			}
			if err != nil {
				return VMExecResult{}, guestExecError(ctx, "testenv Incus guest output transport failed", err)
			}
		case result := <-completed:
			if result.err != nil {
				return VMExecResult{}, guestExecError(ctx, "testenv Incus guest completion unverified", result.err)
			}
			final = result.operation
		case <-ctx.Done():
			return VMExecResult{}, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return VMExecResult{}, err
	}
	metadata := final.Metadata
	raw, ok := metadata["return"]
	if !ok {
		return VMExecResult{}, errors.New("testenv Incus guest exit status missing")
	}
	code, err := strconv.Atoi(fmt.Sprint(raw))
	if err != nil || code < 0 || code > 255 {
		return VMExecResult{}, errors.New("testenv Incus guest exit status invalid")
	}
	result, err := output.Result(code)
	verified = err == nil
	return result, err
}

func guestExecError(ctx context.Context, message string, err error) error {
	if ctx.Err() != nil {
		err = ctx.Err()
	} else if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		err = context.DeadlineExceeded
	}
	return safeIncusError(message, err)
}

func pollIncusExec(ctx context.Context, server incusclient.InstanceServer, id string) (*api.Operation, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		op, _, err := server.GetOperation(id)
		if err != nil {
			return nil, err
		}
		if op == nil {
			return nil, errors.New("missing guest operation")
		}
		if op.StatusCode.IsFinal() {
			if op.StatusCode != api.Success {
				return nil, errors.New("guest operation unsuccessful")
			}
			return op, nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (n *nativeIncusVMClient) cancelGuestOperation(ctx context.Context, id string) {
	ctx, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	defer stop()
	_, server, cancel := n.requestClient(ctx)
	defer cancel()
	_ = server.DeleteOperation(id)
}

func dialIncusExecSocket(ctx context.Context, server incusclient.InstanceServer, id, secret string) (*websocket.Conn, error) {
	info, err := server.GetConnectionInfo()
	if err != nil {
		return nil, err
	}
	httpClient, err := server.GetHTTPClient()
	if err != nil {
		return nil, err
	}
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		return nil, errors.New("unsupported trusted Incus websocket transport")
	}
	u, err := url.Parse(info.URL)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return nil, errors.New("unsupported trusted Incus transport scheme")
	}
	u.Path = "/1.0/operations/" + url.PathEscape(id) + "/websocket"
	query := url.Values{"project": {DefaultVMProject}, "secret": {secret}}
	if info.Target != "" {
		query.Set("target", info.Target)
	}
	u.RawQuery = query.Encode()
	dialer := websocket.Dialer{NetDialContext: transport.DialContext, TLSClientConfig: transport.TLSClientConfig, HandshakeTimeout: 5 * time.Second}
	if info.SocketPath != "" {
		dialer.NetDialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", info.SocketPath)
		}
	}
	conn, response, err := dialer.DialContext(ctx, u.String(), nil)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetReadDeadline(deadline)
		_ = conn.SetWriteDeadline(deadline)
	}
	return conn, nil
}

var _ io.Writer = guestStreamWriter{}
