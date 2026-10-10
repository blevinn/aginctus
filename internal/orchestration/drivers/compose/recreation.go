package compose

import (
	"fmt"
	"sort"
	"strings"
)

// ComposeRecreationPlan is a non-destructive inventory for replacing a
// drifted instance. It never triggers stop, delete, or create operations.
type ComposeRecreationPlan struct {
	Required bool
	PersistentDevices []string
}

// planComposeRecreation inventories disk devices that a future instance
// replacement must preserve. Unknown disk layouts fail closed.
func planComposeRecreation(drift *ComposeDriftError, devices map[string]map[string]string) (ComposeRecreationPlan, error) {
	if drift == nil {
		return ComposeRecreationPlan{}, nil
	}
	plan := ComposeRecreationPlan{Required: true}
	for name, values := range devices {
		if values["type"] != "disk" {
			continue
		}
		path := values["path"]
		if path == "" {
			return ComposeRecreationPlan{}, fmt.Errorf("cannot plan recreation: disk %q has no mount path", name)
		}
		if path == "/" {
			continue
		}
		source, pool := values["source"], values["pool"]
		if source == "" || pool == "" || strings.TrimSpace(source) != source || strings.TrimSpace(pool) != pool {
			return ComposeRecreationPlan{}, fmt.Errorf("cannot plan recreation: disk %q lacks an explicit valid volume reference", name)
		}
		plan.PersistentDevices = append(plan.PersistentDevices, name)
	}
	sort.Strings(plan.PersistentDevices)
	return plan, nil
}
