package testenv

import (
	"context"
	"errors"
	"os"
	"time"
)

// RunRecoveryOnce is the sole externally callable watchdog operation.
// Runtime roots come from a trusted installed systemd unit, never from
// model/MCP requests. It requires a separate unprivileged Unix worker.
func RunRecoveryOnce(ctx context.Context, stateRoot, delegatedCgroupRoot string) (RecoverySummary, error) {
	if os.Geteuid() == 0 {
		return RecoverySummary{}, errors.New("testenv recovery must not run as root")
	}
	if stateRoot == "" || delegatedCgroupRoot == "" {
		return RecoverySummary{}, errors.New("trusted watchdog state and cgroup roots are required")
	}
	store, err := OpenRunStore(stateRoot)
	if err != nil {
		return RecoverySummary{}, err
	}
	manager, err := OpenCgroupManager(delegatedCgroupRoot)
	if err != nil {
		return RecoverySummary{}, err
	}
	return RecoverAbandoned(ctx, store, manager, time.Now().UTC())
}
