package native

import (
	"fmt"
	"sort"
)

const (
	ManagedKey  = "user.aginctus.managed"
	ResourceKey = "user.aginctus.resource"
)

type Action string

const (
	Noop   Action = "noop"
	Create Action = "create"
	Update Action = "update"
	Delete Action = "delete"
)

type Observed struct {
	Exists bool
	Config map[string]string
}
type Change struct {
	Resource ID
	Action   Action
	Keys     []string // changed managed keys only; values must not leak into diagnostics
}

// Plan computes an in-memory, non-mutating change set for explicitly selected
// resources. Observe is supplied by the caller; no Incus calls occur here.
// The caller must perform full preflight before execution.
func Plan(resources []Resource, current map[ID]Observed) ([]Change, error) {
	if err := ValidateSelection(resources); err != nil {
		return nil, err
	}
	changes := make([]Change, 0, len(resources))
	for _, r := range resources {
		actual := current[r.ID()]
		change := Change{Resource: r.ID(), Action: Noop}
		if !actual.Exists {
			if r.State != Absent {
				change.Action = Create
			}
			changes = append(changes, change)
			continue
		}
		// No adopting unlabeled or mismatched resources: even planned deletion
		// requires proof of ownership.
		if actual.Config[ManagedKey] != "true" || actual.Config[ResourceKey] != string(r.Kind) {
			return nil, fmt.Errorf("native %s %q: ownership mismatch", r.Kind, r.Name)
		}
		if r.State == Absent {
			change.Action = Delete
			changes = append(changes, change)
			continue
		}
		for key, want := range r.Config {
			if key == ManagedKey || key == ResourceKey {
				return nil, fmt.Errorf("native %s %q: reserved ownership key %q", r.Kind, r.Name, key)
			}
			if actual.Config[key] != want {
				change.Keys = append(change.Keys, key)
			}
		}
		sort.Strings(change.Keys)
		if len(change.Keys) > 0 {
			change.Action = Update
		}
		changes = append(changes, change)
	}
	return changes, nil
}
