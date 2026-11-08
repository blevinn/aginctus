package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/blevinn/aginctus/internal/testenv"
)

// The batch action delegates to the established run-scoped evidence runner.
// Test injection proves unsupported CLI forms never start a VM.
var executeTestenvVM = testenv.RunVMTest
var serveTestenvMCP = testenv.ServeMCP

func runTestenv(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "mcp" {
		if err := serveTestenvMCP(ctx, os.Stdin, stdout); err != nil {
			fmt.Fprintf(stderr, "testenv mcp: %v\n", err)
			return 1
		}
		return 0
	}
	if len(args) == 0 || args[0] != "vm" {
		fmt.Fprintln(stderr, "invalid testenv invocation; see aginctus testenv --help")
		return 2
	}
	if len(args) == 2 && args[1] == "policy" {
		// Offline canonical policy only. Does not connect to or modify Incus.
		policy, err := testenv.IncusProjectPolicy("testenv")
		if err != nil {
			fmt.Fprintln(stderr, "testenv Incus policy unavailable")
			return 1
		}
		data, err := json.MarshalIndent(policy, "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, "testenv Incus policy encoding unavailable")
			return 1
		}
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	if len(args) == 2 && args[1] == "backend" {
		// Offline discovery; no access to the Incus Unix socket or pilot config.
		data, err := json.MarshalIndent(testenv.SupportedVMBackends(), "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, "testenv backend discovery unavailable")
			return 1
		}
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	if len(args) == 3 && args[1] == "backend" {
		descriptor, err := testenv.VMBackendDescriptor(args[2])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		data, err := json.MarshalIndent(descriptor, "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, "testenv backend discovery unavailable")
			return 1
		}
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	if len(args) == 2 && args[1] == "list" {
		for _, scenario := range testenv.VMScenarios() {
			fmt.Fprintf(stdout, "%s\t%s\n", scenario.Name, scenario.Description)
		}
		return 0
	}
	if len(args) == 3 && args[1] == "plan" {
		scenario, err := testenv.VMScenario(args[2])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		data, err := json.MarshalIndent(scenario, "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, "encode testenv scenario: unavailable")
			return 1
		}
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	if len(args) == 3 && args[1] == "test" {
		if _, err := testenv.VMScenario(args[2]); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		root, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(stderr, "testenv: cannot determine repository working directory")
			return 1
		}
		if err := executeTestenvVM(ctx, root, args[2], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "testenv: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintln(stderr, "invalid testenv invocation; see aginctus testenv --help")
	return 2
}

