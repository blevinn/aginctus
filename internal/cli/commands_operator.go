package cli

import (
	"io"

	operatorcmd "github.com/blevinn/aginctus/internal/cli/commands/operator"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

// Only binding logic belongs at root; owner, confirmation and project
// argument checks remain within the operator subtree.
func addOperatorCommands(root *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int, options *config.Options,
) {
	operatorcmd.Register(root, stdout, stderr, load, status, options, operatorcmd.Handlers{
		UnitsAudit: runOperatorUnitsAudit,
		PackageAudit: runOperatorPackageAudit,
		StateAudit: runOperatorStateAudit,
		Preflight: runOperatorPreflight,
		ProjectEnsure: runOperatorProjectEnsure,
		ProjectPlan: runOperatorProjectPlan,
	})
}
