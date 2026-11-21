package main

import (
	"context"
	"fmt"
	"os"

	"github.com/blevinn/aginctus/internal/plugins/composition"
	"github.com/blevinn/aginctus/internal/cli"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/incus"
)

func main() {
	ctx := context.Background()
	registry, err := appplugins.New(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "plugins: %v\n", err)
		os.Exit(1)
	}
	os.Exit(cli.RunWithPlugins(
		ctx,
		os.Args[1:],
		os.Stdout,
		os.Stderr,
		incus.NewClient(),
		config.NewLoader(),
		registry,
	))
}
