// Operator-only disposable worker acceptance. Not part of aginctus CLI/MCP.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/blevinn/aginctus/internal/testenv"
)

var revision = "source-unverified"

func run(ctx context.Context, args []string, stdout, stderr io.Writer,
	accept func(context.Context, testenv.IncusAcceptanceOptions) (testenv.IncusAcceptanceReport, error)) int {
	var cfg testenv.IncusAcceptanceOptions
	flags := flag.NewFlagSet("aginctus-testenv-acceptance", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&cfg.Acknowledgement, "disposable-worker", "", "required: DEDICATED_DISPOSABLE_WORKER (not isolation proof)")
	flags.StringVar(&cfg.SocketPath, "socket", "", "required: dedicated /var/lib/incus/unix.socket")
	flags.StringVar(&cfg.Project, "project", "", "required: aginctus-testenv")
	flags.StringVar(&cfg.ImageFingerprint, "image", "", "required: imported immutable 64-hex image fingerprint")
	flags.StringVar(&cfg.PolicyKeyPath, "policy-key-file", "", "required: private runtime worker credential")
	flags.StringVar(&cfg.ReportRoot, "report-root", "", "required: existing worker-owned 0700 evidence directory")
	flags.StringVar(&cfg.ForeignInstance, "foreign-instance", "", "required: existing unsigned foreign VM sentinel outside testenv- namespace")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected positional arguments")
		return 2
	}
	cfg.Revision = revision
	r, err := accept(ctx, cfg)
	// Only bounded aggregate metadata reaches stdout, never handles, paths,
	// guest output, foreign instance config, credentials or daemon diagnostics.
	status := "pass"
	if err != nil {
		status = "fail"
		fmt.Fprintln(stderr, "worker acceptance incomplete or unverified; inspect private evidence")
	}
	writeErr := json.NewEncoder(stdout).Encode(struct {
		SchemaVersion int    `json:"schema_version"`
		Profile       string `json:"profile"`
		Status        string `json:"status"`
		RunID         string `json:"run_id"`
	}{1, "dedicated_worker", status, r.RunID})
	if err != nil || writeErr != nil {
		return 1
	}
	return 0
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr, testenv.RunTrustedIncusAcceptance))
}
