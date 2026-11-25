// Test-only OCI entrypoint: no application processes or network listeners.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const handshake = "/fixture-started"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	switch {
	case len(args) == 0:
		shutdown := make(chan os.Signal, 1)
		signal.Notify(shutdown, syscall.SIGTERM, syscall.SIGPWR, syscall.SIGINT)
		if err := os.WriteFile(handshake, nil, 0o644); err != nil {
			return err
		}
		<-shutdown
		return os.WriteFile("/fixture-stopped", []byte("shutdown signal handled\n"), 0o644)
	case len(args) == 1 && args[0] == "started":
		for range 50 {
			if _, err := os.Stat(handshake); !os.IsNotExist(err) {
				return err
			}
			time.Sleep(100 * time.Millisecond)
		}
		return fmt.Errorf("fixture PID 1 did not register its signal handlers")
	case len(args) == 3 && args[0] == "seed":
		f, err := os.OpenFile(args[1], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := f.WriteString(args[2]); err != nil {
			return err
		}
		return f.Sync()
	default:
		return fmt.Errorf("unexpected stop fixture arguments")
	}
}
