// Package incus includes the explicit Incus project scope policy.
// The v0.1 runtime remains unscoped until #200's controlled migration.
// Do not interpret this planning contract as authorization to mutate Incus.
package incus

import (
	"fmt"
	"regexp"
)

const DefaultAginctusProject = "aginctus"

var projectNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)

// ProjectScope is the selected single-deployment Incus namespace. Never
// implicitly resolve a blank project to "default" or mutate Incus remotes.
type ProjectScope struct {
	Name string
}

func NewProjectScope(name string) (ProjectScope, error) {
	if !projectNamePattern.MatchString(name) || name == "default" {
		return ProjectScope{}, fmt.Errorf("Incus project must be explicit, valid and non-default")
	}
	return ProjectScope{Name: name}, nil
}

func (scope ProjectScope) DesiredConfig() map[string]string {
	return map[string]string{
		"user.aginctus.managed":    "true",
		"user.aginctus.resource":   "deployment-project",
		"user.aginctus.deployment": scope.Name,
		"features.images":          "true",
		"features.profiles":        "true",
		"features.storage.volumes": "true",
		"features.networks":        "false",
	}
}

type ProjectAssessment string

const (
	ProjectCompatible ProjectAssessment = "compatible"
	ProjectForeign    ProjectAssessment = "foreign"
	ProjectDrift      ProjectAssessment = "policy-drift"
)

// AssessProject never mutates a project or trusts its name alone.
// Foreign ownership is separate from drift so a caller cannot "repair"
// somebody else's project merely because its feature flags differ.
func (scope ProjectScope) AssessProject(observedName string, observed map[string]string) ProjectAssessment {
	desired := scope.DesiredConfig()
	if observedName != scope.Name ||
		observed["user.aginctus.managed"] != desired["user.aginctus.managed"] ||
		observed["user.aginctus.resource"] != desired["user.aginctus.resource"] ||
		observed["user.aginctus.deployment"] != desired["user.aginctus.deployment"] {
		return ProjectForeign
	}
	for _, key := range []string{
		"features.images", "features.profiles", "features.storage.volumes", "features.networks",
	} {
		if observed[key] != desired[key] {
			return ProjectDrift
		}
	}
	return ProjectCompatible
}
