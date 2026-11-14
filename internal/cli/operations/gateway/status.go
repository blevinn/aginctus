package gatewayops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gateway"
)

type StatusProject struct {
	Name   string            `json:"name"`
	Config map[string]string `json:"config"`
}

type StatusInstance struct {
	Name   string            `json:"name"`
	Status string            `json:"status"`
	Config map[string]string `json:"config"`
}


// Status reports the gateway's observed services using explicitly supplied
// read-only Incus queries and fails on foreign instances or ownership markers.
func Status(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config, inspectProjects func(context.Context) ([]byte, error), inspectInstances func(context.Context, string) ([]byte, error)) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "Usage: aginctus gateway status")
		return 2
	}
	spec, err := gateway.FromConfig(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "gateway configuration: %v\n", err)
		return 1
	}
	raw, err := inspectProjects(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "gateway status: %v\n", err)
		return 1
	}
	var projects []StatusProject
	if err := json.Unmarshal(raw, &projects); err != nil {
		fmt.Fprintf(stderr, "gateway status: decode projects: %v\n", err)
		return 1
	}
	for _, p := range projects {
		if p.Name != spec.Project {
			continue
		}
		if p.Config["user.aginctus.managed"] != "true" || p.Config["user.aginctus.resource"] != "compose-project" || p.Config["user.aginctus.deployment"] != spec.Project {
			fmt.Fprintf(stderr, "gateway project %q exists but ownership markers do not match\n", spec.Project)
			return 1
		}
		// Inspect actual service instances, not merely the presence of a project.
		raw, err = inspectInstances(ctx, spec.Project)
		if err != nil {
			fmt.Fprintf(stderr, "gateway status: %v\n", err)
			return 1
		}
		var instances []StatusInstance
		if err = json.Unmarshal(raw, &instances); err != nil {
			fmt.Fprintf(stderr, "gateway status: decode instances: %v\n", err)
			return 1
		}
		running := 0
		for _, instance := range instances {
			if !StatusService(instance.Name) || instance.Config["user.aginctus.managed"] != "true" || instance.Config["user.aginctus.resource"] != "compose-instance" || instance.Config["user.aginctus.deployment"] != spec.Project {
				fmt.Fprintf(stderr, "gateway project %q contains unexpected or unowned instance %q\n", spec.Project, instance.Name)
				return 1
			}
			if strings.EqualFold(instance.Status, "running") {
				running++
			}
		}
		if len(instances) == 0 {
			fmt.Fprintf(out, "gateway %q: inactive (no service instances; persistent data retained)\n", spec.ID)
			return 0
		}
		fmt.Fprintf(out, "gateway %q: present (%d/%d service instances running; application readiness not verified)\n", spec.ID, running, len(instances))
		return 0
	}
	fmt.Fprintf(out, "gateway %q: absent\n", spec.ID)
	return 0
}

func StatusService(name string) bool {
	for _, service := range []string{"postgres", "litellm"} {
		if name == service {
			return true
		}
		if strings.HasPrefix(name, service+"-") {
			suffix := strings.TrimPrefix(name, service+"-")
			if suffix != "" && suffix[0] != '0' {
				valid := true
				for _, ch := range suffix {
					if ch < '0' || ch > '9' {
						valid = false
						break
					}
				}
				if valid {
					return true
				}
			}
		}
	}
	return false
}
