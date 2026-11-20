package workload

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/pkg/api"
)

// ScopedStatusReader composes domain-owned configuration lookup with the
// verified project-scoped Incus inspection seam. It never reads all projects
// or claims an environment's authoritative accepted-state scope.
type ScopedStatusReader struct {
	Config   *config.Config
	Verify   func(context.Context, incusscope.ProjectScope) error
	Inspect  func(context.Context, incusscope.ProjectScope) ([]byte, error)
	MaxBytes int
}

// ReadScopedWorkloadStatus returns only safe public observations. It resolves
// and checks all selected workload ownership markers before returning any
// result, so collisions do not yield partial inventories.
func (r ScopedStatusReader) ReadScopedWorkloadStatus(ctx context.Context, request api.ScopedWorkloadStatusRequest) (api.ScopedWorkloadStatusList, error) {
	if err := ctx.Err(); err != nil {
		return api.ScopedWorkloadStatusList{}, err
	}
	if r.Config == nil || request.Project == "" {
		return api.ScopedWorkloadStatusList{}, fmt.Errorf("explicit verified project and configuration required")
	}
	ids := []string{}
	if request.Name != "" {
		ids = append(ids, request.Name)
	} else {
		value, found := r.Config.Get("workloads")
		if found {
			configured, ok := value.(map[string]any)
			if !ok {
				return api.ScopedWorkloadStatusList{}, fmt.Errorf("invalid configured workload inventory")
			}
			for name := range configured {
				ids = append(ids, name)
			}
		}
		sort.Strings(ids)
	}
	// Validate configuration before reading the project, and keep all
	// untrusted instance configuration out of the public projection.
	specs := make([]Spec, 0, len(ids))
	for _, id := range ids {
		spec, err := FromConfig(r.Config, id)
		if err != nil {
			return api.ScopedWorkloadStatusList{}, fmt.Errorf("invalid workload configuration for %q: %w", id, err)
		}
		specs = append(specs, spec)
	}
	_, instances, err := ReadScopedManagedInstances(ctx, r.Config, request.Project, r.Verify, r.Inspect, r.MaxBytes)
	if err != nil {
		return api.ScopedWorkloadStatusList{}, err
	}
	result := api.ScopedWorkloadStatusList{
		TypeMeta: api.NewTypeMeta("ScopedWorkloadStatusList"),
		Project: request.Project, Items: []api.ScopedWorkloadStatus{},
	}
	for _, spec := range specs {
		if err := ctx.Err(); err != nil {
			return api.ScopedWorkloadStatusList{}, err
		}
		state, err := FindScopedManagedInstance(instances, spec.Name, "workload", "agent-workload", spec.ID)
		if err != nil {
			return api.ScopedWorkloadStatusList{}, err
		}
		if request.RunningOnly && !strings.EqualFold(state, "running") {
			continue
		}
		if state == "" {
			state = "absent"
		}
		result.Items = append(result.Items, api.ScopedWorkloadStatus{
			Name: spec.ID, Instance: spec.Name, State: state,
		})
	}
	return result, nil
}
