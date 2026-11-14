package operatorops

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

type UnitEvidence string

const (
	UnitAbsent  UnitEvidence = "absent"
	UnitPresent UnitEvidence = "installed"
	UnitActive  UnitEvidence = "active"
	UnitUnknown UnitEvidence = "unverified"
)

func ClassifyUnit(data []byte) UnitEvidence {
	// Treat missing, duplicate, inconsistent and unsupported replies as
	// unverified; they are never evidence that a competing unit is absent.
	props := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok || (key != "LoadState" && key != "ActiveState" && key != "UnitFileState") {
			return UnitUnknown
		}
		if _, exists := props[key]; exists {
			return UnitUnknown
		}
		props[key] = val
	}
	load, loadOK := props["LoadState"]
	active, activeOK := props["ActiveState"]
	_, unitFileOK := props["UnitFileState"]
	if !loadOK || !activeOK || !unitFileOK {
		return UnitUnknown
	}
	if load == "not-found" && (active == "inactive" || active == "") && props["UnitFileState"] == "" {
		return UnitAbsent
	}
	if load != "loaded" {
		return UnitUnknown
	}
	switch active {
	case "active", "activating", "reloading", "deactivating":
		return UnitActive
	case "inactive", "failed":
		// A loaded, disabled, failed or static unit is still a possible
		// controller; don't equate not-running with not-installed.
		return UnitPresent
	default:
		return UnitUnknown
	}
}

func InspectUnitEvidence(ctx context.Context, manager, unit string, inspect func(context.Context, string, string) ([]byte, error)) UnitEvidence {
	data, err := inspect(ctx, manager, unit)
	if err != nil || len(data) > 4096 {
		return UnitUnknown
	}
	return ClassifyUnit(data)
}

func ParseUnitsAuditArgs(args []string) (string, bool) {
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

// RunUnitsAudit is a bounded, read-only diagnostic for the current
// user's manager and the host system manager. It cannot enumerate other user
// managers, aliases, manually started CLI jobs or identify who owns Incus.
// It is NOT a replacement for mutation-time guards in future user units.
func RunUnitsAudit(ctx context.Context, args []string, stdout, stderr io.Writer, euid func() int, lifecycleUnits []string, inspect func(context.Context, string, string) ([]byte, error)) int {
	owner, valid := ParseUnitsAuditArgs(args)
	if !valid {
		fmt.Fprintln(stderr, "Usage: aginctus operator units-audit --owner system|user")
		return 2
	}
	if euid() == 0 {
		fmt.Fprintln(stderr, "operator units-audit: run as the unprivileged operator to inspect that user's manager; no sudo fallback")
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return RenderUnitsAudit(ctx, owner, stdout, stderr, lifecycleUnits, inspect)
}

func RenderUnitsAudit(ctx context.Context, owner string, stdout, stderr io.Writer, lifecycleUnits []string, inspect func(context.Context, string, string) ([]byte, error)) int {
	if owner != "system" && owner != "user" {
		fmt.Fprintln(stderr, "operator units-audit: invalid selected authority")
		return 2
	}
	conflict, incomplete := false, false
	fmt.Fprintf(stdout, "Selected lifecycle authority: %s (read-only; current user only)\n", owner)
	for _, unit := range lifecycleUnits {
		system := InspectUnitEvidence(ctx, "system", unit, inspect)
		user := InspectUnitEvidence(ctx, "user", unit, inspect)
		fmt.Fprintf(stdout, "%s: system=%s user=%s\n", unit, system, user)
		if system == UnitUnknown || user == UnitUnknown {
			incomplete = true
		}
		other := user
		if owner == "user" {
			other = system
		}
		if other == UnitActive || other == UnitPresent {
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
