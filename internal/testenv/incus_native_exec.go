package testenv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
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
	buffer *guestOutputBuffer
	stdout bool
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
	output := &guestOutputBuffer{}
	streamsDone := make(chan bool)
	exec := api.InstanceExecPost{
		Command: argv, WaitForWS: true, Interactive: false,
		RecordOutput: false,
		Environment: map[string]string{"PATH": "/run/current-system/sw/bin:/usr/bin:/bin", "LANG": "C"},
	}
	op, err := n.server.ExecInstance(instance, exec, &incusclient.InstanceExecArgs{
		Stdin:  nil,
		Stdout: guestStreamWriter{buffer: output, stdout: true},
		Stderr: guestStreamWriter{buffer: output},
		DataDone: streamsDone,
	})
	if err != nil {
		return VMExecResult{}, errors.New("testenv Incus guest agent execution unavailable")
	}
	if err := op.WaitContext(ctx); err != nil {
		_ = op.Cancel() // best effort; VM TTL cleanup remains mandatory
		return VMExecResult{}, errors.New("testenv Incus guest execution completion not verified")
	}
	select {
	case <-streamsDone:
	case <-ctx.Done():
		_ = op.Cancel()
		return VMExecResult{}, errors.New("testenv Incus guest output transport deadline exceeded")
	}
	metadata := op.Get().Metadata
	raw, ok := metadata["return"]
	if !ok {
		return VMExecResult{}, errors.New("testenv Incus guest exit status missing")
	}
	code, err := strconv.Atoi(fmt.Sprint(raw))
	if err != nil || code < 0 || code > 255 {
		return VMExecResult{}, errors.New("testenv Incus guest exit status invalid")
	}
	return output.Result(code)
}

var _ io.Writer = guestStreamWriter{}
