package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// This runner is deliberately restricted to read operations in runOperatorPreflight.
// Never use incus project switch: the ambient project belongs to the caller.
var inspectOperatorIncus = func(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "incus", args...).Output()
}

var operatorProjectName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)

func runOperatorPreflight(ctx context.Context, args []string, stdout, stderr io.Writer) int {
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
	if len(args) != 3 || args[0] != "preflight" || args[1] != "--project" ||
		!operatorProjectName.MatchString(args[2]) {
		fmt.Fprintln(stderr, "operator preflight: specify a valid explicit Incus project name")
		return 2
	}
	project := args[2]
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Both probes are GET-only Incus CLI commands. Suppress raw CLI output
	// on failure: remote errors may contain addresses, certificates, or tokens.
	if _, err := inspectOperatorIncus(ctx, "info"); err != nil {
		fmt.Fprintln(stderr, "Incus daemon: unavailable or operator access denied; check the selected remote and authorization (no sudo fallback)")
		return 1
	}
	fmt.Fprintln(stdout, "Incus daemon: readable")

	out, err := inspectOperatorIncus(ctx, "list", "--project", project, "--format", "json")
	if err != nil {
		fmt.Fprintf(stderr, "Incus project %q: cannot list instances; check project visibility and credentials\n", project)
		return 1
	}
	var instances []json.RawMessage
	if err := json.Unmarshal(out, &instances); err != nil {
		fmt.Fprintf(stderr, "Incus project %q: invalid response from read-only instance list\n", project)
		return 1
	}
	fmt.Fprintf(stdout, "Incus project: %s (instance read allowed)\n", project)

	// Project enumeration is an optional observation, not an authorization
	// oracle. Restricted users can see only their permitted projects; a
	// successful project read can never prove mutation or host authority.
	out, err = inspectOperatorIncus(ctx, "project", "list", "--format", "json")
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
	fmt.Fprintln(stdout, "Write permissions: unverified; no resource changes were attempted")
	fmt.Fprintln(stdout, "Host bridge, firewall, pool and admin permissions: unverified; require separately approved host authorization")
	return 0
}
