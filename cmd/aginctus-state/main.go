// Command aginctus-state serves a narrow, same-UID, read-only view of
// local accepted-state revisions. It does not bootstrap Incus resources,
// expose a network listener or authorize acceptance/reconciliation.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/blevinn/aginctus/internal/acceptedstate"
)

func run(ctx context.Context, stateDir, socket string) error {
	if stateDir == "" || socket == "" {
		return errors.New("both --state-dir and --socket are required")
	}
	store, err := acceptedstate.OpenLocalStore(stateDir)
	if err != nil {
		return fmt.Errorf("open private accepted-state directory: %w", err)
	}
	server, err := acceptedstate.StartReadOnlyUnixService(ctx, store, socket)
	if err != nil {
		return fmt.Errorf("start read-only Unix service: %w", err)
	}
	defer server.Close()
	<-ctx.Done()
	return nil
}

func main() {
	dir := flag.String("state-dir", "", "preinitialized private accepted-state directory (absolute)")
	socket := flag.String("socket", "", "Unix socket in an owned mode-0700 directory (absolute)")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *dir, *socket); err != nil {
		fmt.Fprintln(os.Stderr, "aginctus-state:", err)
		os.Exit(1)
	}
}
