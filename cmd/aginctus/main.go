package main

import (
	"context"
	"os"

	"github.com/blevinn/aginctus/internal/cli"
	"github.com/blevinn/aginctus/internal/incus"
)

func main() {
	os.Exit(cli.Run(
		context.Background(),
		os.Args[1:],
		os.Stdout,
		os.Stderr,
		incus.NewClient(),
	))
}
