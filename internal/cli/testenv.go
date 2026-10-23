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
		printTestenvUsage(stderr)
		return 2
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
	printTestenvUsage(stderr)
	return 2
}

func printTestenvUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: aginctus testenv mcp  (stdio MCP 2026-07-28; read-only VM discovery)")
	fmt.Fprintln(w, "Usage: aginctus testenv vm list  (offline; no config or Incus)")
	fmt.Fprintln(w, "       aginctus testenv vm plan SCENARIO  (read-only pinned target)")
	fmt.Fprintln(w, "       aginctus testenv vm test SCENARIO  (opt-in heavyweight NixOS VM)")
	fmt.Fprintln(w, "Batch test reuses the checkout Python runner/evidence. For trusted operators")
	fmt.Fprintln(w, "on a dedicated unprivileged test worker ONLY; not an agent sandbox.")
	fmt.Fprintln(w, "Persistent guest exec/reset/destroy and MCP execution tools are not implemented yet.")
}
