package testenv

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestIncusCreateFailureStage(t *testing.T) {
	for _, tc := range []struct {
		message string
		err     error
		want    string
	}{
		{"private path: target disk too small", nil, "create_wait_size"},
		{"no space left on device", nil, "create_wait_capacity"},
		{"qemu-img: private path", nil, "create_wait_image"},
		{"permission denied", nil, "create_wait_permission"},
		{"failed to create logical volume", nil, "create_wait_storage"},
		{"private unknown provider diagnostic", nil, "create_wait"},
		{"private unknown provider diagnostic", context.DeadlineExceeded, "create_wait_timeout"},
	} {
		stage := incusCreateWaitStage("create_wait", tc.message, tc.err)
		if stage != tc.want {
			t.Fatalf("unexpected stage: %s, want %s", stage, tc.want)
		}
		native := withIncusCreateStage(stage, errors.New("safe native error"), nil)
		outer := withIncusCreateStage("create_request", ErrOwnershipMismatch, native)
		if incusCreateStage(outer) != tc.want || !errors.Is(outer, ErrOwnershipMismatch) ||
			strings.Contains(outer.Error(), "private") {
			t.Fatal("lost phase, changed ownership classification, or disclosed provider text")
		}
	}
}
