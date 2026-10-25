package testenv

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Incus guest-agent readiness is a distinct admission step from a successful
// Incus start operation. Never infer guest readiness from "Running" alone.
// These bounds also keep an unavailable guest from tying up a worker forever.
const (
	MaxIncusAgentReadiness    = 2 * time.Minute
	incusAgentProbeTimeout    = 2 * time.Second
	incusAgentProbeInterval   = 100 * time.Millisecond
	incusAgentProbeExecutable = "/run/current-system/sw/bin/true"
)

var ErrIncusAgentNotReady = errors.New("testenv Incus VM agent did not become ready")

// WaitForIncusVMAgent verifies the signed original run and checks an actual
// guest-only command through the Incus agent. The probe is fixed and never
// comes from the coding agent, checkout, shell, or untrusted environment.
//
// A timeout does NOT authorize deletion and does not imply the VM has gone.
// The worker must keep the sealed run handle for verified cleanup/TTL recovery.
func (b *IncusVMBackend) WaitForIncusVMAgent(ctx context.Context, ref VMRef, wait time.Duration) error {
	if b == nil || b.client == nil {
		return ErrBackendUnavailable
	}
	if wait <= 0 || wait > MaxIncusAgentReadiness {
		return errors.New("testenv Incus agent readiness timeout outside admitted bounds")
	}
	if _, ok := b.client.(GuestExecClient); !ok {
		return ErrUnsupportedAction
	}
	if err := b.verifyRef(ref); err != nil {
		return err
	}
	runCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()

	for {
		if err := runCtx.Err(); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return ErrIncusAgentNotReady
		}
		state, expiry, err := b.loadOwned(runCtx, ref)
		if err != nil {
			// Unowned/deleted/malformed resources may NOT become trusted by
			// retrying. Transport failures are not proof of ownership either.
			return err
		}
		if !strings.EqualFold(state.Status, "Running") {
			return ErrIncusAgentNotReady
		}
		if !b.now().Before(expiry) {
			return ErrIncusAgentNotReady
		}
		remaining := time.Until(deadlineOrAfter(runCtx, incusAgentProbeTimeout))
		if remaining > incusAgentProbeTimeout {
			remaining = incusAgentProbeTimeout
		}
		if remaining <= 0 {
			return ErrIncusAgentNotReady
		}
		output, err := b.Exec(runCtx, ref, []string{incusAgentProbeExecutable}, remaining)
		if err == nil {
			if output.ExitCode == 0 && output.Stdout == "" && output.Stderr == "" {
				return nil
			}
			// A responding agent that cannot run the known immutable
			// probe is not merely slow to become ready.
			return ErrIncusAgentNotReady
		}
		if errors.Is(err, ErrOwnershipMismatch) ||
			errors.Is(err, ErrUnsupportedAction) ||
			errors.Is(err, ErrBackendUnavailable) {
			return err
		}
		if runCtx.Err() != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return ErrIncusAgentNotReady
		}
		timer := time.NewTimer(incusAgentProbeInterval)
		select {
		case <-runCtx.Done():
			timer.Stop()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return ErrIncusAgentNotReady
		case <-timer.C:
		}
	}
}

func deadlineOrAfter(ctx context.Context, d time.Duration) time.Time {
	deadline := time.Now().Add(d)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		return contextDeadline
	}
	return deadline
}

// CreateReadyIncusVM is the trusted worker's intended admission path.
// An indeterminate start/readiness error returns the original sealed handle
// so a separate worker recovery pass can authenticate cleanup later.
func (b *IncusVMBackend) CreateReadyIncusVM(ctx context.Context, spec VMCreateSpec, readinessWait time.Duration) (VMRef, error) {
	if readinessWait <= 0 || readinessWait > MaxIncusAgentReadiness {
		return VMRef{}, errors.New("testenv Incus readiness timeout outside admitted bounds")
	}
	if b == nil || b.client == nil {
		return VMRef{}, ErrBackendUnavailable
	}
	if _, ok := b.client.(GuestExecClient); !ok {
		return VMRef{}, ErrUnsupportedAction
	}
	ref, err := b.Create(ctx, spec)
	if err != nil {
		return ref, err
	}
	if err := b.WaitForIncusVMAgent(ctx, ref, readinessWait); err != nil {
		return ref, err
	}
	return ref, nil
}
