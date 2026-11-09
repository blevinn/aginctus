package acceptedstate

import (
	"errors"

	"github.com/blevinn/aginctus/internal/config"
)

// FromConfigured projects only explicit deployment, Incus project and selected
// workload identities from an already resolved Config. The caller supplies
// deployment identity and selection explicitly; unselected workloads and all
// other config (including privileged policy, image settings, and credentials)
// are never copied. This read-only projection is NOT a full accepted spec,
// a source-authorization check, or permission to reconcile Incus.
func FromConfigured(cfg *config.Config, deploymentID string, selected []string) (SelectionManifest, error) {
	if cfg == nil {
		return SelectionManifest{}, errors.New("configuration is required")
	}
	scope, err := cfg.IncusProjectScope()
	if err != nil {
		return SelectionManifest{}, errors.New("configured Incus project is invalid")
	}
	manifest := SelectionManifest{
		APIVersion:        SelectionAPIVersion,
		Kind:              SelectionKind,
		DeploymentID:      deploymentID,
		IncusProject:      scope.Name,
		SelectedWorkloads: append([]string{}, selected...),
	}
	// Do not treat a nil caller selection as an explicitly empty selection.
	if selected == nil {
		manifest.SelectedWorkloads = nil
	}
	if err := manifest.Validate(); err != nil {
		return SelectionManifest{}, err
	}
	raw, ok := cfg.Get("workloads")
	if !ok {
		return SelectionManifest{}, errors.New("configured workloads are absent")
	}
	declared, ok := raw.(map[string]any)
	if !ok {
		return SelectionManifest{}, errors.New("configured workloads must be an object")
	}
	for _, id := range manifest.SelectedWorkloads {
		workload, exists := declared[id]
		if !exists {
			return SelectionManifest{}, errors.New("selected workload is not configured")
		}
		if _, valid := workload.(map[string]any); !valid {
			return SelectionManifest{}, errors.New("selected workload must be a configuration object")
		}
	}
	return manifest, nil
}
