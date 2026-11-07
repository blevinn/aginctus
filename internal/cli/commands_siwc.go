package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	gatewayassets "github.com/blevinn/aginctus/scripts/gateway"
)

// runSIWC is intentionally independent of host/Incus configuration. Enrollment
// and token operations run only in the explicitly selected private local store.
func runSIWC(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 2 && args[0] == "bundle" {
		dir := args[1]
		if !filepath.IsAbs(dir) {
			fmt.Fprintln(stderr, "SIWC bundle requires an absolute directory")
			return 2
		}
		if err := os.Mkdir(dir, 0700); err != nil {
			fmt.Fprintln(stderr, "SIWC bundle destination must be a new directory")
			return 1
		}
		for name, content := range gatewayassets.SIWCFiles() {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
				fmt.Fprintln(stderr, "SIWC bundle delivery failed")
				return 1
			}
		}
		fmt.Fprintln(stdout, "SIWC public runtime bundle written; enrollment and activation are separate operations")
		return 0
	}
	command := exec.CommandContext(ctx, "python3", append([]string{"-I", "-c", gatewayassets.SIWCSource}, args...)...)
	command.Stdout, command.Stderr = stdout, stderr
	command.Stdin = os.Stdin
	if err := command.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		fmt.Fprintln(stderr, "SIWC requires Python 3 with PyJWT and cryptography (included by the Nix package)")
		return 1
	}
	return 0
}
