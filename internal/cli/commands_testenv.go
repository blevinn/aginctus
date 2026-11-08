package cli

import (
	"io"

	testenvcmd "github.com/blevinn/aginctus/internal/cli/commands/testenv"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

// Keep the root binding in cli; the public command grammar lives beside its
// command hierarchy in commands/testenv. No privileged worker leaves attach here.
func addTestenvCommands(root *cobra.Command, stdout, stderr io.Writer,
	status *int, options *config.Options,
) {
	testenvcmd.Register(root, stdout, stderr, status, options, runTestenv)
}
