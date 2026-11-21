// Command aginctus-project-state-fixture is an opt-in same-UID bootstrap and
// inspection tool for the project-owned Incus-volume acceptance experiment.
// It NEVER authenticates Incus, grants project rights or runs workloads.
// No production CLI, Home Manager or NixOS activation invokes this command.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
)

type options struct {
	stateDir      string
	serverID      string
	project       string
	environmentID string
	candidate     string
	expected      string
	confirm       bool
	socket        string
}

type acceptedStatus struct {
	HasAccepted bool                          `json:"hasAccepted"`
	Revision    *environment.AcceptedRevision `json:"revision,omitempty"`
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	if ctx == nil {
		return errors.New("operation requires context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(args) == 0 {
		return errors.New("explicit init, status, audit, accept or serve required")
	}
	action := args[0]
	if action != "init" && action != "status" && action != "audit" && action != "accept" && action != "serve" {
		return errors.New("unsupported project-state fixture action")
	}
	fs := flag.NewFlagSet(action, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var o options
	fs.StringVar(&o.stateDir, "state-dir", "", "preselected absolute private volume state path")
	fs.StringVar(&o.serverID, "server-id", "", "explicit test server identity (NOT authenticated)")
	fs.StringVar(&o.project, "project", "", "explicit nondefault Incus project")
	fs.StringVar(&o.environmentID, "environment", "", "immutable environment UUID")
	fs.StringVar(&o.candidate, "candidate", "", "absolute typed environment candidate JSON")
	fs.StringVar(&o.expected, "expected-revision", "", "exact published accepted revision ID or none")
	fs.BoolVar(&o.confirm, "confirm", false, "explicit same-UID fixture acceptance; does not deploy")
	fs.StringVar(&o.socket, "socket", "", "absolute Unix socket path under a private precreated directory")
	if err := fs.Parse(args[1:]); err != nil {
		return errors.New("invalid fixture command flags")
	}
	if fs.NArg() != 0 || o.stateDir == "" || o.serverID == "" || o.project == "" || o.project == "default" {
		return errors.New("absolute state directory, explicit server ID and nondefault project required")
	}
	binding := environment.Binding{ServerID: o.serverID, Project: o.project}
	if action == "init" {
		if o.environmentID != "" || o.candidate != "" || o.expected != "" || o.confirm || o.socket != "" {
			return errors.New("init accepts only project fixture identity")
		}
		_, err := environment.InitializeLocalProjectStateStore(o.stateDir, binding)
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(struct {
			Initialized bool `json:"initialized"`
		}{true})
	}
	if action != "serve" && o.environmentID == "" {
		return errors.New("explicit immutable environment UUID required")
	}
	store, err := environment.OpenLocalProjectStateStore(o.stateDir, binding)
	if err != nil {
		return err
	}
	if action == "serve" {
		if o.environmentID != "" || o.candidate != "" || o.expected != "" || o.confirm || o.socket == "" {
			return errors.New("serve requires only --socket and a preinitialized state volume")
		}
		server, err := environment.StartLocalProjectReadOnly(ctx, store, o.socket)
		if err != nil {
			return err
		}
		defer server.Close()
		<-ctx.Done()
		return nil
	}
	if o.socket != "" {
		return errors.New("socket selector is only allowed with serve")
	}
	// Only the UUID participates in fixture lookup. A synthetic local name
	// cannot authorize or rename the enrolled environment's accepted record.
	desc := environment.Descriptor{ID: o.environmentID, Name: "fixture-read", Binding: binding}
	if err := desc.Validate(); err != nil {
		return errors.New("invalid environment selection")
	}
	if action == "audit" {
		if o.candidate != "" || o.expected != "" || o.confirm {
			return errors.New("audit is read-only and accepts no promotion flags")
		}
		result, err := store.AuditLocalPublishedHistory(ctx, desc)
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(result)
	}
	if action == "status" {
		if o.candidate != "" || o.expected != "" || o.confirm {
			return errors.New("status is read-only and accepts no promotion flags")
		}
		rev, _, err := store.ReadLocalAccepted(ctx, desc)
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(acceptedStatus{HasAccepted: rev != nil, Revision: rev})
	}
	if !o.confirm || o.expected == "" || o.candidate == "" {
		return errors.New("fixture acceptance requires --confirm, --candidate and --expected-revision")
	}
	candidate, err := environment.ReadCandidateFile(o.candidate)
	if err != nil {
		return err
	}
	if candidate.Environment.ID != o.environmentID || candidate.Environment.Binding != binding {
		return errors.New("candidate targets a different environment or project binding")
	}
	current, _, err := store.ReadLocalAccepted(ctx, candidate.Environment)
	if err != nil {
		return err
	}
	var expected *environment.AcceptedRevision
	switch {
	case o.expected == "none":
		if current != nil {
			return environment.ErrAcceptedConflict
		}
	case current == nil || current.ID != o.expected:
		return environment.ErrAcceptedConflict
	default:
		expected = current
	}
	// CommitLocalCandidate rechecks the full expected revision inside its
	// own lock. The observed binding here is caller-supplied and is NOT
	// evidence of Incus authentication: THIS BINARY MUST REMAIN A FIXTURE.
	next, err := store.CommitLocalCandidate(ctx, candidate.Environment, binding, expected, candidate)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(acceptedStatus{HasAccepted: true, Revision: &next})
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "aginctus-project-state-fixture:", err)
		os.Exit(1)
	}
}
