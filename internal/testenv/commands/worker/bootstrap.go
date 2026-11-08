package worker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/testenv"
	"github.com/spf13/cobra"
)

// PlannerInput is bounded offline operator data, never daemon-derived trust.
type PlannerInput struct {
	Contract testenv.IncusBootstrapContract `json:"contract"`
	Inventory testenv.IncusBootstrapInventory `json:"inventory"`
}

var ErrOperatorAction = errors.New("offline bootstrap needs operator action")

func readPlannerInput(r io.Reader) (PlannerInput, error) {
	const limit = 256 * 1024
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil || len(data) > limit {
		return PlannerInput{}, errors.New("invalid or oversized offline planner input")
	}
	var in PlannerInput
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil || decoder.Decode(new(any)) != io.EOF {
		return PlannerInput{}, errors.New("invalid offline planner input")
	}
	return in, nil
}

// newBootstrap contains no Incus client, subprocess access or key material.
func newBootstrap() *cobra.Command {
	bootstrap := &cobra.Command{
		Use: "bootstrap", Short: "Plan worker bootstrap without contacting Incus",
		RunE: groupRun,
	}
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

	return bootstrap
}
