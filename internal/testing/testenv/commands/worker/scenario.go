package worker

import (
	"encoding/json"

	"github.com/blevinn/aginctus/internal/testing/testenv"
	"github.com/spf13/cobra"
)

// This offline projection belongs to the developer worker command tree.
// No runtime handler, worker credentials, Incus or host shell is involved.
func newScenario() *cobra.Command {
	root := &cobra.Command{Use: "scenario", Short: "Plan a managed disposable scenario", RunE: groupRun}
	root.AddCommand(&cobra.Command{
		Use:  "plan SCENARIO",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			plan, err := testenv.PlanManagedScenario(args[0])
			if err != nil {
				return err
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(plan)
		},
	})
	return root
}
