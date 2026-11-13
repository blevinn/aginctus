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

	"github.com/blevinn/aginctus/internal/validation/evidence"
	"github.com/blevinn/aginctus/internal/validation/runner"
)

func execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("aginctus-check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	format := flags.String("format", "text", "text or json")
	verbose := flags.Bool("verbose", false, "stream unfiltered diagnostic output; local trusted use only")
	vmRunID := flags.String("vm-run-id", "", "inspect an existing private VM receipt; never start a VM")
	acceptanceID := flags.String("acceptance-run-id", "", "inspect an existing private acceptance receipt")
	coverageMode := flags.Bool("coverage", false, "compose explicitly selected existing evidence only")
	hermeticJSON := flags.String("hermetic-json", "", "existing bounded hermetic JSON file for coverage")
	coverageVMID := flags.String("coverage-vm-run-id", "", "existing private VM run ID for coverage")
	pilotJSON := flags.String("pilot-topology-json", "", "existing pilot topology preflight JSON for coverage")
	runtimeJSON := flags.String("opencode-runtime-json", "", "existing runtime preflight JSON for coverage")
	localID := flags.String("local-acceptance-run-id", "", "existing private local acceptance run ID for coverage")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return 2
	}
	vmSelected, acceptanceSelected, hasCoverageSource := false, false, false
	invalidEmptySource := false
	flags.Visit(func(entry *flag.Flag) {
		switch entry.Name {
		case "vm-run-id":
			vmSelected = true
		case "acceptance-run-id":
			acceptanceSelected = true
		case "hermetic-json", "coverage-vm-run-id", "pilot-topology-json", "opencode-runtime-json", "local-acceptance-run-id":
			hasCoverageSource = true
			if entry.Value.String() == "" {
				invalidEmptySource = true
			}
		}
	})
	if *format != "text" && *format != "json" {
		fmt.Fprintln(stderr, "format must be text or json")
		return 2
	}
	if *format == "json" && *verbose {
		fmt.Fprintln(stderr, "--verbose cannot be combined with --format json")
		return 2
	}
	if *verbose && (vmSelected || acceptanceSelected || *coverageMode) {
		fmt.Fprintln(stderr, "--verbose is unavailable for evidence reports")
		return 2
	}
	if invalidEmptySource || (hasCoverageSource && !*coverageMode) ||
		(vmSelected && (acceptanceSelected || *coverageMode)) ||
		(acceptanceSelected && *coverageMode) {
		fmt.Fprintln(stderr, "conflicting or incomplete evidence selection")
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
	if vmSelected {
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
	if acceptanceSelected {
		view, err := evidence.ReadAcceptance(os.Getenv("AGINCTUS_ACCEPTANCE_REPORT_ROOT"), *acceptanceID)
		if err != nil {
			view = evidence.UnavailableAcceptance()
		}
		if *format == "json" {
			err = evidence.WriteJSON(stdout, view)
		} else {
			err = evidence.WriteAcceptanceText(stdout, view)
		}
		if err != nil {
			fmt.Fprintln(stderr, "unable to write acceptance evidence report")
		}
		// Partial acceptance is never a successful live sign-off.
		return 1
	}
	if *coverageMode {
		view := evidence.NormalizeCoverage(evidence.CoverageSelection{
			HermeticJSON: *hermeticJSON, VMRunID: *coverageVMID,
			VMRoot: filepath.Join(root, ".vm-harness-results"),
			PilotTopologyJSON: *pilotJSON, OpenCodeRuntimeJSON: *runtimeJSON,
			AcceptanceRunID: *localID,
			AcceptanceRoot: os.Getenv("AGINCTUS_ACCEPTANCE_REPORT_ROOT"),
		})
		if *format == "json" {
			err = evidence.WriteJSON(stdout, view)
		} else {
			err = evidence.WriteCoverageText(stdout, view)
		}
		if err != nil {
			fmt.Fprintln(stderr, "unable to write coverage evidence report")
		}
		// By design this profile cannot independently prove live acceptance.
		return 1
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
