// aginctus-testenv provides operator-only test environment tools.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/blevinn/aginctus/internal/testenv"
	"github.com/spf13/cobra"
)

type plannerInput struct {
	Contract  testenv.IncusBootstrapContract  `json:"contract"`
	Inventory testenv.IncusBootstrapInventory `json:"inventory"`
}

var errOperatorAction = errors.New("offline bootstrap needs operator action")

func readPlannerInput(r io.Reader) (plannerInput, error) {
	const limit = 256 * 1024
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil || len(data) > limit {
		return plannerInput{}, errors.New("invalid or oversized offline planner input")
	}
	var in plannerInput
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil || decoder.Decode(new(any)) != io.EOF {
		return plannerInput{}, errors.New("invalid offline planner input")
	}
	return in, nil
}

func newRootCommand(stdin io.Reader, stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "aginctus-testenv",
		Short:         "Operator tools for isolated test environments",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)

	groupRun := func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("%s requires a subcommand", cmd.CommandPath())
		}
		return fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())
	}
	worker := &cobra.Command{
		Use: "worker", Short: "Inspect or prepare a dedicated test environment worker",
		RunE: groupRun,
	}
	bootstrap := &cobra.Command{
		Use: "bootstrap", Short: "Plan worker bootstrap without contacting Incus",
		RunE: groupRun,
	}
	worker.AddCommand(bootstrap)
	root.AddCommand(worker)

	bootstrap.AddCommand(&cobra.Command{
		Use:   "plan",
		Short: "Validate an operator-owned bootstrap contract and inventory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			in, err := readPlannerInput(cmd.InOrStdin())
			if err != nil {
				return err
			}
			plan, planErr := testenv.PlanIncusWorkerBootstrap(in.Contract, in.Inventory)
			if err := json.NewEncoder(cmd.OutOrStdout()).Encode(plan); err != nil {
				return errors.New("could not write offline bootstrap plan")
			}
			if planErr != nil {
				return errOperatorAction
			}
			return nil
		},
	})
	bootstrap.AddCommand(&cobra.Command{
		Use:   "fingerprints",
		Short: "Emit inspection-only inventory and pool fingerprint candidates",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			in, err := readPlannerInput(cmd.InOrStdin())
			if err != nil {
				return err
			}
			digest, err := testenv.BootstrapInventoryFingerprint(in.Inventory)
			if err != nil {
				return errors.New("offline inventory fingerprint unavailable")
			}
			poolDigest, err := testenv.BootstrapPoolConfigFingerprint(in.Contract.Pool)
			if err != nil {
				return errors.New("offline pool fingerprint unavailable")
			}
			result := map[string]string{
				"status": "inspection_only", "inventory_fingerprint": digest,
				"pool_config_fingerprint": poolDigest,
			}
			if err := json.NewEncoder(cmd.OutOrStdout()).Encode(result); err != nil {
				return errors.New("could not write offline fingerprints")
			}
			return nil
		},
	})
	return root
}

func execute(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := newRootCommand(stdin, stdout, stderr)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(stderr, err)
		if errors.Is(err, errOperatorAction) {
			return 1
		}
		return 2
	}
	return 0
}

func main() {
	os.Exit(execute(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
