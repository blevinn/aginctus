package operatorops

import (
	"encoding/json"
	"fmt"
	"strings"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// ObservedProject describes the exact policy-relevant portion of a project
// inspection. Restricted enumeration cannot establish global absence.
type ObservedProject struct {
	Name   string            `json:"name"`
	Config map[string]string `json:"config"`
}


// ParseProjectArgs validates an explicitly selected non-default project.
func ParseProjectArgs(args []string) (incusscope.ProjectScope, error) {
	project := ""
	switch {
	case len(args) == 1 && strings.HasPrefix(args[0], "--project="):
		project = strings.TrimPrefix(args[0], "--project=")
	case len(args) == 2 && args[0] == "--project":
		project = args[1]
	default:
		return incusscope.ProjectScope{}, fmt.Errorf("explicit --project NAME is required")
	}
	return incusscope.NewProjectScope(project)
}


// AssessObservedProjects is a read-only, fail-closed policy assessment.
func AssessObservedProjects(scope incusscope.ProjectScope, raw []byte) (string, error) {
	var projects []ObservedProject
	if err := json.Unmarshal(raw, &projects); err != nil || projects == nil {
		return "", fmt.Errorf("invalid incus project enumeration")
	}
	status := "not-visible"
	matches := 0
	for _, project := range projects {
		if project.Name != scope.Name {
			continue
		}
		matches++
		status = string(scope.AssessProject(project.Name, project.Config))
	}
	if matches > 1 {
		return "", fmt.Errorf("duplicate incus project identity")
	}
	return status, nil
}
