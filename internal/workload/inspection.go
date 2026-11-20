package workload

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// InspectedInstance is a narrow inventory DTO. It is never a public API
// resource and does not itself establish ownership or authorization.
type InspectedInstance struct {
	Name    string            `json:"name"`
	Status  string            `json:"status"`
	Config  map[string]string `json:"config"`
	Project string            `json:"project"`
}

// ReadScopedManagedInstances verifies the configured Incus project before
// inspecting it and refuses oversized, ambiguous or mismatched inventories.
// The caller supplies its established project ownership verifier and Incus
// inspection capabilities; this helper never falls back to a default project.
func ReadScopedManagedInstances(
	ctx context.Context, cfg *config.Config, project string,
	verify func(context.Context, incusscope.ProjectScope) error,
	inspect func(context.Context, incusscope.ProjectScope) ([]byte, error),
	maxBytes int,
) (incusscope.ProjectScope, []InspectedInstance, error) {
	if cfg == nil {
		return incusscope.ProjectScope{}, nil, fmt.Errorf("project configuration unavailable")
	}
	scope, err := cfg.IncusProjectScope()
	if err != nil || scope.Name != project {
		return incusscope.ProjectScope{}, nil, fmt.Errorf("project must match the declared incus.project setting")
	}
	if verify == nil || inspect == nil || maxBytes <= 0 {
		return incusscope.ProjectScope{}, nil, fmt.Errorf("project-scoped inspection capabilities unavailable")
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

// FindScopedManagedInstance fails when a same-name instance has unexpected
// ownership labels. Absence is distinct from collision and must not allow
// adoption of foreign resources.
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
