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

// RunUnitsAudit is a bounded, read-only inventory of the caller's user
// manager and the host system manager. Neither manager is an Incus project
// authority, and this diagnostic cannot enumerate other users or manual writers.
func RunUnitsAudit(ctx context.Context, args []string, stdout, stderr io.Writer, euid func() int, lifecycleUnits []string, inspect func(context.Context, string, string) ([]byte, error)) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "Usage: aginctus operator units-audit")
		return 2
	}
	if euid() == 0 {
		fmt.Fprintln(stderr, "operator units-audit: run as the unprivileged operator to inspect that user's manager; no sudo fallback")
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return RenderUnitsAudit(ctx, stdout, stderr, lifecycleUnits, inspect)
}

// RenderUnitsAudit reports observable unit states without selecting or
// claiming a lifecycle owner. Unknown/denied observations fail conservatively.
func RenderUnitsAudit(ctx context.Context, stdout, stderr io.Writer, lifecycleUnits []string, inspect func(context.Context, string, string) ([]byte, error)) int {
	incomplete := false
	fmt.Fprintln(stdout, "Known Aginctus unit inventory (read-only; no controller authority inferred)")
	for _, unit := range lifecycleUnits {
		system := InspectUnitEvidence(ctx, "system", unit, inspect)
		user := InspectUnitEvidence(ctx, "user", unit, inspect)
		fmt.Fprintf(stdout, "%s: system=%s user=%s\n", unit, system, user)
		if system == UnitUnknown || user == UnitUnknown {
			incomplete = true
		}
	}
	if incomplete {
		fmt.Fprintln(stderr, "unit inventory incomplete or unverified; no writer authority established")
		return 1
	}
	fmt.Fprintln(stdout, "Unit observations complete for system and current user only; not a controller-conflict check or write authorization")
	return 0
}
