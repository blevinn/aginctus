package testenv

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxIncusExecArgs = 32
	maxIncusExecArgBytes = 8 * 1024
	maxIncusExecDuration = 30 * time.Second
)

var ErrGuestOutputLimit = errors.New("testenv guest output exceeded limit")

func validateIncusExec(argv []string, duration time.Duration) error {
	if duration <= 0 || duration > maxIncusExecDuration {
		return errors.New("testenv guest execution duration is outside supported bounds")
	}
	if len(argv) == 0 || len(argv) > maxIncusExecArgs {
		return errors.New("testenv guest argv count is outside supported bounds")
	}
	if !filepath.IsAbs(argv[0]) || filepath.Clean(argv[0]) != argv[0] {
		return errors.New("testenv guest executable must have a clean absolute path")
	}
	size := 0
	for _, arg := range argv {
		if strings.IndexByte(arg, 0) >= 0 {
			return errors.New("testenv guest argv contains a NUL byte")
		}
		size += len(arg)
		if size > maxIncusExecArgBytes {
			return errors.New("testenv guest argv exceeds byte limit")
		}
	}
	return nil
}

// GuestExecClient is a separate optional capability so lifecycle adapters
// cannot silently gain executable guest authority through their constructors.
type GuestExecClient interface {
	ExecGuest(context.Context, string, []string) (VMExecResult, error)
}

// Exec executes argv ONLY inside the owned Incus VM through incus-agent.
// It does not expose the Incus socket to the user and never runs a host shell.
func (b *IncusVMBackend) Exec(ctx context.Context, ref VMRef, argv []string, timeout time.Duration) (VMExecResult, error) {
	if b == nil || b.client == nil {
		return VMExecResult{}, ErrBackendUnavailable
	}
	if err := validateIncusExec(argv, timeout); err != nil {
		return VMExecResult{}, err
	}
	guest, ok := b.client.(GuestExecClient)
	if !ok {
		return VMExecResult{}, ErrUnsupportedAction
	}
	instance, expires, err := b.loadOwned(ctx, ref)
	if err != nil {
		return VMExecResult{}, err
	}
	if !strings.EqualFold(instance.Status, "Running") {
		return VMExecResult{}, errors.New("testenv guest VM is not running")
	}
	if !b.now().Before(expires) || b.now().Add(timeout).After(expires) {
		return VMExecResult{}, errors.New("guest exec cannot exceed VM run lifetime")
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := guest.ExecGuest(runCtx, ref.Instance, append([]string(nil), argv...))
	if errors.Is(err, ErrGuestOutputLimit) {
		return VMExecResult{}, ErrGuestOutputLimit
	}
	if err != nil {
		return VMExecResult{}, errors.New("testenv guest execution transport failed or timed out")
	}
	if len(result.Stdout)+len(result.Stderr) > MaxGuestOutput {
		return VMExecResult{}, ErrGuestOutputLimit
	}
	return result, nil
}
