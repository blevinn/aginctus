// aginctus-plugin-gen generates application-owned static plugin composition.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/blevinn/aginctus/internal/pluginbuild"
)

func run(args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("aginctus-plugin-gen", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifest := flags.String("manifest", "", "JSON build manifest path")
	out := flags.String("out", "", "generated Go source path")
	check := flags.Bool("check", false, "verify generated source without writing")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *manifest == "" || *out == "" || flags.NArg() != 0 {
		return fmt.Errorf("-manifest and -out are required; no positional arguments")
	}
	raw, err := os.ReadFile(*manifest)
	if err != nil {
		return err
	}
	source, err := pluginbuild.Generate(raw)
	if err != nil {
		return err
	}
	if *check {
		current, err := os.ReadFile(*out)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, source) {
			return fmt.Errorf("generated composition is stale; regenerate from the build manifest")
		}
		return nil
	}
	return os.WriteFile(*out, source, 0644)
}

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
