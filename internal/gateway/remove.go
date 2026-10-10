package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// runIncus is replaceable in tests. Each invocation uses the local Incus CLI.
var runIncus = func(ctx context.Context, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "incus", args...)
	out, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("incus %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

type cleanupProject struct {
	Name   string            `json:"name"`
	Config map[string]string `json:"config"`
}
type cleanupInstance struct {
	Name   string            `json:"name"`
	Config map[string]string `json:"config"`
}

// Only the two known gateway services may be removed. The Compose naming
// convention is project + service (+ optional replica number).
func gatewayServiceName(project, name string) bool {
	return name == project+"-postgres" || name == project+"-litellm" ||
		strings.HasPrefix(name, project+"-postgres-") && numericSuffix(name, project+"-postgres-") ||
		strings.HasPrefix(name, project+"-litellm-") && numericSuffix(name, project+"-litellm-")
}
func numericSuffix(name, prefix string) bool {
	return regexp.MustCompile(`^[0-9]+$`).MatchString(strings.TrimPrefix(name, prefix))
}

func validateGatewayCleanup(project string, p cleanupProject, instances []cleanupInstance, force bool) error {
	if project == "" || p.Name != project {
		return fmt.Errorf("gateway cleanup project mismatch")
	}
	owned := p.Config["user.aginctus.managed"] == "true" &&
		p.Config["user.aginctus.resource"] == "compose-project" &&
		p.Config["user.aginctus.deployment"] == project
	if !owned && !force {
		return fmt.Errorf("gateway project %q is not owned by this deployment; inspect it and use gateway remove --force only for a confirmed stale gateway", project)
	}
	for _, instance := range instances {
		if !gatewayServiceName(project, instance.Name) {
			return fmt.Errorf("refusing to delete unexpected instance %q in gateway project %q", instance.Name, project)
		}
		instanceOwned := instance.Config["user.aginctus.managed"] == "true" &&
			instance.Config["user.aginctus.resource"] == "compose-instance" &&
			instance.Config["user.aginctus.deployment"] == project
		if !instanceOwned && !force {
			return fmt.Errorf("gateway instance %q lacks matching ownership metadata", instance.Name)
		}
	}
	return nil
}

// Remove only gateway service instances. Volumes, images, and networks are
// intentionally not deleted; data retention takes precedence over tidiness.
func (s Spec) Remove(ctx context.Context, force bool) error {
	if err := s.Validate(); err != nil {
		return err
	}
	// Limit forced recovery to an unmistakably Aginctus-named project.
	if force && !strings.HasPrefix(s.Project, "aginctus-") {
		return fmt.Errorf("forced gateway cleanup requires an aginctus-prefixed project name, got %q", s.Project)
	}
	raw, err := runIncus(ctx, "project", "list", "--format=json")
	if err != nil {
		return fmt.Errorf("inspect gateway project: %w", err)
	}
	var projects []cleanupProject
	if err := json.Unmarshal(raw, &projects); err != nil {
		return fmt.Errorf("decode gateway project: %w", err)
	}
	var project cleanupProject
	for _, candidate := range projects {
		if candidate.Name == s.Project {
			project = candidate
			break
		}
	}
	if project.Name == "" {
		return fmt.Errorf("gateway project %q not found", s.Project)
	}
	instancesRaw, err := runIncus(ctx, "list", "--project", s.Project, "--format=json")
	if err != nil {
		return fmt.Errorf("list gateway instances: %w", err)
	}
	var instances []cleanupInstance
	if err := json.Unmarshal(instancesRaw, &instances); err != nil {
		return fmt.Errorf("decode gateway instances: %w", err)
	}
	if err := validateGatewayCleanup(s.Project, project, instances, force); err != nil {
		return err
	}
	for _, instance := range instances {
		if _, err := runIncus(ctx, "delete", instance.Name, "--project", s.Project, "--force"); err != nil {
			return fmt.Errorf("delete gateway instance %q: %w", instance.Name, err)
		}
	}
	// Explicit stale-deployment recovery transitions only this named project
	// to the same ownership markers used by the Compose driver. This makes
	// the subsequent ordinary apply possible without a global bypass.
	if force {
		for _, entry := range []string{
			"user.aginctus.managed=true",
			"user.aginctus.resource=compose-project",
			"user.aginctus.deployment=" + s.Project,
		} {
			if _, err := runIncus(ctx, "project", "set", s.Project, entry); err != nil {
				return fmt.Errorf("mark recovered gateway project: %w", err)
			}
		}
	}
	return nil
}
