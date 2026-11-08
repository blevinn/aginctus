// aginctus-check is a developer/CI-only hermetic runner, not the product CLI.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/blevinn/aginctus/internal/validation/runner"
	"github.com/blevinn/aginctus/internal/validation/evidence"
)

func execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("aginctus-check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	format := flags.String("format", "text", "text or json")
	verbose := flags.Bool("verbose", false, "stream unfiltered diagnostic output; local trusted use only")
	vmRunID := flags.String("vm-run-id", "", "inspect an existing private VM receipt; never start a VM")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return 2
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintln(stderr, "format must be text or json")
		return 2
	}
	if *format == "json" && *verbose {
		fmt.Fprintln(stderr, "--verbose cannot be combined with --format json")
		return 2
	}
	if *vmRunID != "" && *verbose {
		fmt.Fprintln(stderr, "--verbose is unavailable for VM evidence")
		return 2
	}
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "unable to resolve working directory")
		return 1
	}
	if _, err := os.Stat("justfile"); err != nil {
		fmt.Fprintln(stderr, "aginctus-check requires the repository root")
		return 1
	}
	if *vmRunID != "" {
		vm, err := evidence.ReadVM(filepath.Join(root, ".vm-harness-results"), *vmRunID)
		if err != nil {
			vm = evidence.Unavailable()
		}
		if *format == "json" {
			err = evidence.WriteJSON(stdout, vm)
		} else {
			err = evidence.WriteVMText(stdout, vm)
		}
		if err != nil {
			fmt.Fprintln(stderr, "unable to write VM evidence report")
			return 1
		}
		if vm["status"] != "pass" {
			return 1
		}
		return 0
	}
	report := runner.Run(ctx, root, os.Environ(), *verbose, stdout)
	if *format == "json" {
		err = runner.WriteJSON(stdout, report)
	} else {
		err = runner.WriteText(stdout, report)
	}
	if err != nil {
		fmt.Fprintln(stderr, "unable to write validation report")
		return 1
	}
	if report.Status != "pass" {
		return 1
	}
	return 0
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(execute(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
