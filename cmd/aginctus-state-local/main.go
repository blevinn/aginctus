// Command aginctus-state-local is an explicit same-principal, offline
// accepted-state proof of concept. It NEVER runs as a root/user proxy,
// connects to Incus or reconciles workloads. The deployment service and its
// independent authentication/recovery contract remain separate work.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/blevinn/aginctus/internal/acceptedstate"
)

type localOptions struct {
	stateDir   string
	deployment string
	project    string
	candidate  string
	expected   string
	confirm    bool
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("expected init, status, diff, or accept")
	}
	action := args[0]
	if action != "init" && action != "status" && action != "diff" && action != "accept" {
		return errors.New("unknown local state action")
	}
	fs := flag.NewFlagSet(action, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var opts localOptions
	fs.StringVar(&opts.stateDir, "state-dir", "", "absolute private single-principal state directory")
	fs.StringVar(&opts.deployment, "deployment", "", "explicit deployment ID")
	fs.StringVar(&opts.project, "project", "", "explicit Incus project")
	fs.StringVar(&opts.candidate, "candidate", "", "explicit pinned workload candidate JSON file")
	fs.StringVar(&opts.expected, "expected-revision", "", "current revision SHA256 or none for first acceptance")
	fs.BoolVar(&opts.confirm, "confirm", false, "explicitly confirm acceptance (does not reconcile infrastructure)")
	if err := fs.Parse(args[1:]); err != nil {
		return errors.New("invalid local-state flags")
	}
	if fs.NArg() != 0 || opts.stateDir == "" {
		return errors.New("explicit --state-dir and no positional arguments required")
	}
	if action == "init" {
		if opts.deployment != "" || opts.project != "" || opts.candidate != "" || opts.expected != "" || opts.confirm {
			return errors.New("init accepts only --state-dir")
		}
		if _, err := acceptedstate.InitializeLocalStore(opts.stateDir); err != nil {
			return err
		}
		return writeJSON(stdout, struct {
			Initialized bool `json:"initialized"`
		}{true})
	}
	if opts.deployment == "" || opts.project == "" {
		return errors.New("explicit --deployment and --project required")
	}
	store, err := acceptedstate.OpenLocalStore(opts.stateDir)
	if err != nil {
		return err
	}
	if action == "status" {
		if opts.candidate != "" || opts.expected != "" || opts.confirm {
			return errors.New("status does not accept candidate, expected revision or confirm flags")
		}
		rev, spec, err := store.ReadActive()
		if err != nil {
			return err
		}
		if rev == nil {
			return writeJSON(stdout, struct {
				HasAccepted bool `json:"hasAccepted"`
			}{false})
		}
		if spec.Selection.DeploymentID != opts.deployment || spec.Selection.IncusProject != opts.project {
			return errors.New("selected deployment or Incus project does not match accepted state")
		}
		return writeJSON(stdout, struct {
			HasAccepted bool                   `json:"hasAccepted"`
			Revision    acceptedstate.Revision `json:"revision"`
		}{true, *rev})
	}
	if opts.candidate == "" {
		return errors.New("diff and accept require an explicit --candidate")
	}
	candidate, err := acceptedstate.ReadCandidateFile(opts.candidate, opts.deployment, opts.project)
	if err != nil {
		return err
	}
	if action == "diff" {
		if opts.expected != "" || opts.confirm {
			return errors.New("diff does not accept expected revision or confirm flags")
		}
		comparison, err := acceptedstate.CompareCandidate(store, candidate)
		if err != nil {
			return err
		}
		return writeJSON(stdout, comparison)
	}
	// A caller's CLI flag is never a cross-principal authorization credential.
	// The store must be private and same-UID; this command never creates its
	// directory implicitly or reaches a privileged daemon.
	if !opts.confirm || opts.expected == "" {
		return errors.New("accept requires --confirm and explicit --expected-revision")
	}
	current, _, err := store.ReadActive()
	if err != nil {
		return err
	}
	var generation uint64
	var id string
	if opts.expected == "none" {
		if current != nil {
			return acceptedstate.ErrRevisionConflict
		}
	} else {
		if current == nil || opts.expected != current.ID {
			return acceptedstate.ErrRevisionConflict
		}
		generation = current.Generation
		id = current.ID
	}
	// The local store rechecks generation and revision ID under its own lock,
	// so any intervening writer causes a CAS conflict rather than overwrite.
	next, err := store.CommitLocal(generation, id, candidate)
	if err != nil {
		return err
	}
	return writeJSON(stdout, next)
}

func writeJSON(w io.Writer, value any) error {
	if w == nil {
		return errors.New("output stream is required")
	}
	if err := json.NewEncoder(w).Encode(value); err != nil {
		return fmt.Errorf("write redacted state result: %w", err)
	}
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "aginctus-state-local:", err)
		os.Exit(1)
	}
}
