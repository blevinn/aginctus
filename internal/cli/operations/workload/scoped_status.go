package workloadops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/herdrclient"
	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/workload"
)

// InspectedInstance is the stable observed Incus identity. The scoped read
// refuses ambiguous names, unexpected projects and foreign ownership markers.
type InspectedInstance struct {
	Name    string            `json:"name"`
	Status  string            `json:"status"`
	Config  map[string]string `json:"config"`
	Project string            `json:"project"`
}



type ScopedStatusActions struct {
	Parse func([]string) (string, []string, error)
	Read func(context.Context, *config.Config, string) (incusscope.ProjectScope, []InspectedInstance, error)
	Find func([]InspectedInstance, string, string, string, string) (string, error)
}

func ScopedInstanceListArgs(scope incusscope.ProjectScope) []string {
	return []string{"--force-local", "--project", scope.Name, "list", "--format", "json"}
}

func ParseScopedInstanceStatusArgs(args []string) (string, []string, error) {
	var name string
	switch {
	case len(args) >= 2 && args[0] == "--project":
		name = args[1]
		args = args[2:]
	case len(args) >= 1 && strings.HasPrefix(args[0], "--project="):
		name = strings.TrimPrefix(args[0], "--project=")
		args = args[1:]
	default:
		return "", nil, fmt.Errorf("explicit --project NAME required")
	}
	if _, err := incusscope.NewProjectScope(name); err != nil {
		return "", nil, err
	}
	return name, args, nil
}

func ReadScopedManagedInstances(ctx context.Context, cfg *config.Config, name string, verify func(context.Context, incusscope.ProjectScope) error, inspect func(context.Context, incusscope.ProjectScope) ([]byte, error), maxBytes int) (incusscope.ProjectScope, []InspectedInstance, error) {
	scope, err := cfg.IncusProjectScope()
	if err != nil || scope.Name != name {
		return incusscope.ProjectScope{}, nil, fmt.Errorf("project must match the declared incus.project setting")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := verify(ctx, scope); err != nil {
		return incusscope.ProjectScope{}, nil, fmt.Errorf("local project ownership and feature policy unverified")
	}
	raw, err := inspect(ctx, scope)
	if err != nil || len(raw) > maxBytes {
		return incusscope.ProjectScope{}, nil, fmt.Errorf("project-scoped instance inventory unavailable")
	}
	var instances []InspectedInstance
	if err := json.Unmarshal(raw, &instances); err != nil || instances == nil {
		return incusscope.ProjectScope{}, nil, fmt.Errorf("invalid project-scoped instance inventory")
	}
	seen := make(map[string]bool, len(instances))
	for _, instance := range instances {
		if instance.Name == "" || (instance.Project != "" && instance.Project != scope.Name) || seen[instance.Name] {
			return incusscope.ProjectScope{}, nil, fmt.Errorf("ambiguous or invalid project-scoped instance inventory")
		}
		seen[instance.Name] = true
	}
	return scope, instances, nil
}

// Unlike legacy findManagedInstance, this lookup deliberately does not
// presume the default project. Ownership is still checked for every named
// instance, and a conflicting name never appears as "absent".
func FindScopedManagedInstance(instances []InspectedInstance, name, resource, role, id string) (string, error) {
	for _, instance := range instances {
		if instance.Name != name {
			continue
		}
		if instance.Config["user.aginctus.managed"] != "true" ||
			instance.Config["user.aginctus.resource"] != resource ||
			(role != "" && instance.Config["user.aginctus.role"] != role) ||
			(id != "" && instance.Config["user.aginctus.workload"] != id) {
			return "", fmt.Errorf("instance %q exists but ownership markers do not match", name)
		}
		return instance.Status, nil
	}
	return "", nil
}

func RunScopedHerdrStatus(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config, actions ScopedStatusActions) int {
	name, rest, err := actions.Parse(args)
	if err != nil || len(rest) != 0 {
		fmt.Fprintln(stderr, "Usage: aginctus herdr client status --project NAME")
		return 2
	}
	spec, err := herdrclient.FromConfig(cfg)
	if err != nil {
		fmt.Fprintln(stderr, "herdr client status: invalid configuration")
		return 1
	}
	_, instances, err := actions.Read(ctx, cfg, name)
	if err != nil {
		fmt.Fprintf(stderr, "herdr client status: %v\n", err)
		return 1
	}
	status, err := actions.Find(instances, spec.Name, "infrastructure", "herdr-client", "")
	if err != nil {
		fmt.Fprintf(stderr, "herdr client status: %v\n", err)
		return 1
	}
	if status == "" {
		status = "absent"
	}
	fmt.Fprintf(out, "Herdr client %q: %s\n", spec.Name, status)
	return 0
}

func RunScopedWorkloadStatus(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config, listOnly bool, actions ScopedStatusActions) int {
	name, rest, err := actions.Parse(args)
	if err != nil || (listOnly && len(rest) != 0) || len(rest) > 1 {
		if listOnly {
			fmt.Fprintln(stderr, "Usage: aginctus workload ls --project NAME")
		} else {
			fmt.Fprintln(stderr, "Usage: aginctus workload status --project NAME [name]")
		}
		return 2
	}
	ids := IDs(cfg)
	if len(rest) == 1 {
		ids = rest
	}
	specs := make([]workload.Spec, 0, len(ids))
	for _, id := range ids {
		spec, err := workload.FromConfig(cfg, id)
		if err != nil {
			fmt.Fprintln(stderr, "workload status: invalid workload configuration")
			return 1
		}
		specs = append(specs, spec)
	}
	_, instances, err := actions.Read(ctx, cfg, name)
	if err != nil {
		fmt.Fprintf(stderr, "workload status: %v\n", err)
		return 1
	}
	// Do not print partial results if a later workload has conflicting
	// ownership. Aggregate all results only after verifying every name.
	results := make([]string, 0, len(specs))
	for _, spec := range specs {
		status, err := actions.Find(instances, spec.Name, "workload", "agent-workload", spec.ID)
		if err != nil {
			fmt.Fprintf(stderr, "workload status: %v\n", err)
			return 1
		}
		if listOnly {
			if strings.EqualFold(status, "running") {
				results = append(results, spec.ID)
			}
			continue
		}
		if status == "" {
			status = "absent"
		}
		results = append(results, fmt.Sprintf("workload %q: %s", spec.ID, status))
	}
	for _, result := range results {
		fmt.Fprintln(out, result)
	}
	return 0
}
