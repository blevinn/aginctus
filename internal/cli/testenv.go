package cli

import (
	"context"
	"io"
	"os"

	testenvops "github.com/blevinn/aginctus/internal/cli/operations/testenv"
	"github.com/blevinn/aginctus/internal/testenv"
)

// Test injection proves malformed CLI invocations do not start a VM.
var executeTestenvVM = testenv.RunVMTest
var serveTestenvMCP = testenv.ServeMCP

func runTestenv(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return testenvops.Run(ctx, args, os.Stdin, stdout, stderr, testenvops.Actions{
		Execute: executeTestenvVM,
		Serve: serveTestenvMCP,
	})
}
