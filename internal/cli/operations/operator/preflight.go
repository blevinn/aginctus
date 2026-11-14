package operatorops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

type PreflightActions struct {
	Read func(context.Context, string) (json.RawMessage, ReadStatus)
	Identity func(context.Context, json.RawMessage, string) (string, bool)
	Inspect func(context.Context, ...string) ([]byte, error)
	Confinement func(context.Context, string) ProjectConfinementEvidence
	ReadFailure func(error) ReadStatus
}

func Preflight(ctx context.Context, args []string, stdout, stderr io.Writer, actions PreflightActions) int {
	if len(args) != 3 || args[0] != "preflight" ||
		(args[1] != "--project" && !strings.HasPrefix(args[1], "--project=")) {
		// Explicit project identity is required until the project-scoped
		// configuration contract (#200) is available. No default-project fallback.
		if len(args) == 2 && args[0] == "preflight" && strings.HasPrefix(args[1], "--project=") {
			args = []string{"preflight", "--project", strings.TrimPrefix(args[1], "--project=")}
		} else {
			fmt.Fprintln(stderr, "Usage: aginctus operator preflight --project NAME")
			return 2
		}
	}
	if len(args) != 3 || args[0] != "preflight" || args[1] != "--project" {
		fmt.Fprintln(stderr, "operator preflight: specify a valid explicit Incus project name")
		return 2
	}
	project := args[2]
	if _, err := incusscope.NewProjectScope(project); err != nil {
		fmt.Fprintln(stderr, "operator preflight: specify a valid explicit nondefault Incus project name")
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	server, status := actions.Read(ctx, "/1.0?project="+url.QueryEscape(project))
	if status != ReadOK {
		fmt.Fprintf(stderr, "Incus daemon: %s; check the selected remote, client installation and separately provisioned authorization (no sudo fallback)\n", status)
		return 1
	}
	fmt.Fprintln(stdout, "Incus daemon: readable")
	identity, trusted := actions.Identity(ctx, server, project)
	fmt.Fprintf(stdout, "Incus identity: %s\n", identity)
	if !trusted || ctx.Err() != nil {
		fmt.Fprintln(stderr, "operator identity is unauthenticated, inconsistent or unproven; ask the host administrator to verify the selected identity")
		return 1
	}

	out, status := actions.Read(ctx, "/1.0/instances?project="+url.QueryEscape(project)+"&recursion=1")
	if status != ReadOK {
		fmt.Fprintf(stderr, "Incus project %q: %s; check project visibility and credentials\n", project, status)
		return 1
	}
	var instances []json.RawMessage
	if err := json.Unmarshal(out, &instances); err != nil || instances == nil {
		fmt.Fprintf(stderr, "Incus project %q: invalid response from read-only instance list\n", project)
		return 1
	}
	fmt.Fprintf(stdout, "Incus project: %s (instance read allowed)\n", project)

	// Project enumeration is an optional observation, not an authorization
	// oracle. Restricted users can see only their permitted projects; a
	// successful project read can never prove mutation or host authority.
	out, err := actions.Inspect(ctx, "project", "list", "--format", "json")
	if err != nil {
		fmt.Fprintln(stdout, "Project enumeration: unavailable (scope cannot be inferred)")
	} else {
		var projects []struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(out, &projects) == nil {
			fmt.Fprintf(stdout, "Visible Incus projects: %d (not an authorization proof)\n", len(projects))
		} else {
			fmt.Fprintln(stdout, "Project enumeration: invalid response (scope cannot be inferred)")
		}
	}
	fmt.Fprintf(stdout, "Project confinement: %s (read-only project-policy evidence; not a write authorization proof)\n", actions.Confinement(ctx, project))
	if ctx.Err() != nil {
		fmt.Fprintf(stderr, "operator preflight: %s; retry the read-only check after restoring daemon access\n", actions.ReadFailure(ctx.Err()))
		return 1
	}
	fmt.Fprintln(stdout, "Write permissions: unverified; no resource changes were attempted")
	fmt.Fprintln(stdout, "Host bridge, firewall, pool and admin permissions: unverified; require separately approved host authorization")
	return 0
}
