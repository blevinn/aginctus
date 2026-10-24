package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// These are the known Aginctus units that can import images or reconcile
// managed resources. Host firewall-only units are deliberately excluded:
// they remain host-owned even when a user owns the workload lifecycle.
var operatorLifecycleUnits = []string{
	"aginctus-image-herdr-client.service",
	"aginctus-image-opencode-workload.service",
	"aginctus-image-update.service",
	"aginctus-reconcile.service",
}

// Query only selected, nonsensitive systemd properties. Never return unit
// environment, command lines, or raw subprocess errors to the operator.
var inspectOperatorUnit = func(ctx context.Context, manager, unit string) ([]byte, error) {
	return exec.CommandContext(ctx, "systemctl", "--"+manager, "show",
		"--no-pager", "--property=LoadState", "--property=ActiveState",
		"--property=UnitFileState", unit).Output()
}

type unitEvidence string

const (
	unitAbsent   unitEvidence = "absent"
	unitPresent  unitEvidence = "installed"
	unitActive   unitEvidence = "active"
	unitUnknown  unitEvidence = "unverified"
)

func classifyOperatorUnit(data []byte) unitEvidence {
	// Treat missing, duplicate, inconsistent and unsupported replies as
	// unverified; they are never evidence that a competing unit is absent.
	props := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok || (key != "LoadState" && key != "ActiveState" && key != "UnitFileState") {
			return unitUnknown
		}
		if _, exists := props[key]; exists {
			return unitUnknown
		}
		props[key] = val
	}
	load, loadOK := props["LoadState"]
	active, activeOK := props["ActiveState"]
	_, unitFileOK := props["UnitFileState"]
	if !loadOK || !activeOK || !unitFileOK {
		return unitUnknown
	}
	if load == "not-found" && (active == "inactive" || active == "") && props["UnitFileState"] == "" {
		return unitAbsent
	}
	if load != "loaded" {
		return unitUnknown
	}
	switch active {
	case "active", "activating", "reloading", "deactivating":
		return unitActive
	case "inactive", "failed":
		// A loaded, disabled, failed or static unit is still a possible
		// controller; don't equate not-running with not-installed.
		return unitPresent
	default:
		return unitUnknown
	}
}

func inspectOperatorUnitEvidence(ctx context.Context, manager, unit string) unitEvidence {
	data, err := inspectOperatorUnit(ctx, manager, unit)
	if err != nil || len(data) > 4096 {
		return unitUnknown
	}
	return classifyOperatorUnit(data)
}

func parseOperatorUnitsAuditArgs(args []string) (string, bool) {
	switch {
	case len(args) == 1 && args[0] == "--owner=system":
		return "system", true
	case len(args) == 1 && args[0] == "--owner=user":
		return "user", true
	case len(args) == 2 && args[0] == "--owner" && (args[1] == "system" || args[1] == "user"):
		return args[1], true
	default:
		return "", false
	}
}

// runOperatorUnitsAudit is a bounded, read-only diagnostic for the current
// user's manager and the host system manager. It cannot enumerate other user
// managers, aliases, manually started CLI jobs or identify who owns Incus.
// It is NOT a replacement for mutation-time guards in future user units.
func runOperatorUnitsAudit(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	owner, valid := parseOperatorUnitsAuditArgs(args)
	if !valid {
		fmt.Fprintln(stderr, "Usage: aginctus operator units-audit --owner system|user")
		return 2
	}
	if os.Geteuid() == 0 {
		fmt.Fprintln(stderr, "operator units-audit: run as the unprivileged operator to inspect that user's manager; no sudo fallback")
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return renderOperatorUnitsAudit(ctx, owner, stdout, stderr)
}

func renderOperatorUnitsAudit(ctx context.Context, owner string, stdout, stderr io.Writer) int {
	if owner != "system" && owner != "user" {
		fmt.Fprintln(stderr, "operator units-audit: invalid selected authority")
		return 2
	}
	conflict, incomplete := false, false
	fmt.Fprintf(stdout, "Selected lifecycle authority: %s (read-only; current user only)\n", owner)
	for _, unit := range operatorLifecycleUnits {
		system := inspectOperatorUnitEvidence(ctx, "system", unit)
		user := inspectOperatorUnitEvidence(ctx, "user", unit)
		fmt.Fprintf(stdout, "%s: system=%s user=%s\n", unit, system, user)
		if system == unitUnknown || user == unitUnknown {
			incomplete = true
		}
		other := user
		if owner == "user" {
			other = system
		}
		if other == unitActive || other == unitPresent {
			conflict = true
		}
	}
	if conflict || incomplete {
		fmt.Fprintln(stderr, "lifecycle ownership conflict or unverified unit manager: do not hand off; inspect active system/user controllers through authorized operators")
		return 1
	}
	fmt.Fprintln(stdout, "No competing known lifecycle units observed in these two managers; other users, Incus aliases, CLI jobs, version compatibility and write authority remain unverified")
	return 0
}
