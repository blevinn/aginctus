// Offline operator tooling. This binary never connects to Incus.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/blevinn/aginctus/internal/testenv"
)

type input struct {
	Contract  testenv.IncusBootstrapContract  `json:"contract"`
	Inventory testenv.IncusBootstrapInventory `json:"inventory"`
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--help" {
		fmt.Fprintln(stdout, "usage: aginctus-testenv-worker-plan [--fingerprints] < operator-input.json\nOffline only; never admission, provisioning or runtime acceptance.")
		return 0
	}
	inspect := len(args) == 1 && args[0] == "--fingerprints"
	if len(args) != 0 && !inspect {
		fmt.Fprintln(stderr, "invalid offline planner arguments")
		return 2
	}
	const limit = 256 * 1024
	data, err := io.ReadAll(io.LimitReader(stdin, limit+1))
	if err != nil || len(data) > limit {
		fmt.Fprintln(stderr, "invalid or oversized offline planner input")
		return 2
	}
	var in input
	// Decode from a bounded buffer; reject unknown fields and trailing values.
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil || decoder.Decode(new(any)) != io.EOF {
		fmt.Fprintln(stderr, "invalid offline planner input")
		return 2
	}
	if inspect {
		digest, err := testenv.BootstrapInventoryFingerprint(in.Inventory)
		if err != nil {
			fmt.Fprintln(stderr, "offline inventory fingerprint unavailable")
			return 2
		}
		poolDigest, err := testenv.BootstrapPoolConfigFingerprint(in.Contract.Pool)
		if err != nil {
			fmt.Fprintln(stderr, "offline pool fingerprint unavailable")
			return 2
		}
		if json.NewEncoder(stdout).Encode(map[string]string{"status": "inspection_only", "inventory_fingerprint": digest, "pool_config_fingerprint": poolDigest}) != nil {
			return 1
		}
		return 0
	}
	plan, err := testenv.PlanIncusWorkerBootstrap(in.Contract, in.Inventory)
	writeErr := json.NewEncoder(stdout).Encode(plan)
	if err != nil || writeErr != nil {
		fmt.Fprintln(stderr, "offline bootstrap needs operator action")
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
