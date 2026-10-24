package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/blevinn/aginctus/internal/testenv"
)

const watchdogDeadline = 45 * time.Second

// Only an installed, root-owned systemd unit should select these values.
// No arguments, arbitrary commands, host shell or MCP transport are accepted.
func configuredRoots(args []string, getenv func(string) string) (string, string, error) {
	if len(args) != 0 {
		return "", "", errors.New("testenv recovery accepts no command-line arguments")
	}
	state := getenv("AGINCTUS_TESTENV_RUN_ROOT")
	cgroups := getenv("AGINCTUS_TESTENV_CGROUP_ROOT")
	if state == "" || cgroups == "" {
		return "", "", errors.New("trusted testenv recovery directories are not configured")
	}
	return state, cgroups, nil
}

func run(ctx context.Context, args []string, getenv func(string) string) error {
	state, cgroot, err := configuredRoots(args, getenv)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, watchdogDeadline)
	defer cancel()
	summary, err := testenv.RunRecoveryOnce(ctx, state, cgroot)
	// Only bounded synthetic counters reach stdout/journal; no guest logs,
	// payloads, path names, provider headers or credentials.
	if data, jsonErr := json.Marshal(summary); jsonErr == nil {
		fmt.Println(string(data))
	}
	return err
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, "testenv recovery failed (inspect trusted worker journal)")
		os.Exit(1)
	}
}
