package configops

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
)

// Run formats the effective configured state after the Cobra leaf's syntax
// checks and root config loader have completed. This operation cannot mutate.
func Run(args []string, stdout, stderr io.Writer, effective *config.Config) int {
	if len(args) == 1 && args[0] == "show" {
		data, err := json.MarshalIndent(effective.Values(), "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "encode configuration: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s\n", data)
		return 0
	}

	if len(args) == 2 && args[0] == "get" {
		value, ok := effective.Get(args[1])
		if !ok {
			fmt.Fprintf(stderr, "configuration key %q is not set\n", args[1])
			return 1
		}
		data, err := json.Marshal(value)
		if err != nil {
			fmt.Fprintf(stderr, "encode configuration value: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s\n", data)
		return 0
	}

	fmt.Fprintln(stderr, "Usage: aginctus [global options] config show")
	fmt.Fprintln(stderr, "       aginctus [global options] config get <path>")
	return 2
}
