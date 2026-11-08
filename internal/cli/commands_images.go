package cli

import (
	"io"

	imagescmd "github.com/blevinn/aginctus/internal/cli/commands/images"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

func addImageCommands(root *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int,
) {
	imagescmd.Register(root, stdout, stderr, load, status, imagescmd.Handlers{
		Legacy: runImages,
		Configured: runConfiguredImages,
	})
}
