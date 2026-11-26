package testenv

import (
	"context"
	"errors"
	"strings"
)

// incusCreateStageError preserves a fixed phase, not the provider's error text.
type incusCreateStageError struct {
	stage string
	err   error
}

func (e *incusCreateStageError) Error() string { return e.err.Error() }
func (e *incusCreateStageError) Unwrap() error { return e.err }

func withIncusCreateStage(stage string, safeErr, original error) error {
	var previous *incusCreateStageError
	if errors.As(original, &previous) {
		stage = previous.stage
	}
	return &incusCreateStageError{stage: stage, err: safeErr}
}

func incusCreateStage(err error) string {
	var staged *incusCreateStageError
	if errors.As(err, &staged) {
		return staged.stage
	}
	return "create"
}

// Classify provider failures into a bounded vocabulary. These hints do not
// prove resource creation or authorize recovery; unknown failures stay generic.
func incusCreateWaitStage(phase, providerMessage string, err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return phase + "_timeout"
	}
	message := strings.ToLower(providerMessage)
	switch {
	case strings.Contains(message, "too small"), strings.Contains(message, "cannot shrink"),
		strings.Contains(message, "smaller than"), strings.Contains(message, "larger than"):
		return phase + "_size"
	case strings.Contains(message, "no space left"), strings.Contains(message, "insufficient free space"),
		strings.Contains(message, "not enough free space"), strings.Contains(message, "insufficient space"):
		return phase + "_capacity"
	case strings.Contains(message, "qemu-img"), strings.Contains(message, "unpack image"),
		strings.Contains(message, "image unpack"):
		return phase + "_image"
	case strings.Contains(message, "permission denied"), strings.Contains(message, "operation not permitted"):
		return phase + "_permission"
	case strings.Contains(message, "logical volume"), strings.Contains(message, "lvcreate"),
		strings.Contains(message, "storage volume"), strings.Contains(message, "thin pool"):
		return phase + "_storage"
	default:
		return phase
	}
}
