package clihelp

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

// WriteCobraExamples prints only reviewed, command-authored examples.
// It must not invent executable invocations for mutating operations.
func WriteCobraExamples(cmd *cobra.Command, out io.Writer) error {
	if cmd.Short != "" {
		if _, err := fmt.Fprintln(out, cmd.Short); err != nil { return err }
	}
	if _, err := fmt.Fprintf(out, "Usage:\n  %s\n", cmd.UseLine()); err != nil { return err }
	if example := strings.TrimSpace(cmd.Example); example != "" {
		_, err := fmt.Fprintf(out, "\nExamples:\n%s\n", example)
		return err
	}
	// Group help is a useful navigation aid, but should never construct
	// potentially mutating invocations with guessed arguments.
	for _, child := range cmd.Commands() {
		if child.Hidden || !child.IsAvailableCommand() { continue }
		if example := strings.TrimSpace(child.Example); example != "" {
			if _, err := fmt.Fprintf(out, "\n%s:\n%s\n", child.Name(), example); err != nil { return err }
		}
	}
	_, err := fmt.Fprintf(out, "\nFor the complete option reference, run 'aginctus help --full%s'.\n", strings.TrimPrefix(cmd.CommandPath(), "aginctus"))
	return err
}
