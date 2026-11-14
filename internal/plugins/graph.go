package plugins

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/blevinn/aginctus/pkg/orchestration"
)

// TargetKey qualifies a public local target ID without rewriting its declaration
// or finding IDs. Scope is explicit; it conveys no resource mutation authority.
type TargetKey struct {
	Deployment string
	Instance   string
	Target     string
}

// String is an unambiguous key for validated components, which cannot contain '/'.
func (k TargetKey) String() string { return k.Deployment + "/" + k.Instance + "/" + k.Target }

// ScopedTarget preserves local definitions and separately resolves dependencies.
// This is not an executor binding, image resolution or accepted-state record.
type ScopedTarget struct {
	Key        TargetKey
	Plugin     string
	Definition orchestration.Target
	DependsOn  []TargetKey
}

// Graph is a detached, complete declaration graph in dependency-first order,
// with lexical scoped-key tie-breaking. An empty selection yields an empty graph.
// Ordering does not mean that prerequisite postconditions have actually passed.
type Graph struct {
	Deployment string
	Targets    []ScopedTarget
}

func assembleGraph(ctx context.Context, deployment string, targets []ScopedTarget) (Graph, error) {
	byKey := make(map[string]ScopedTarget, len(targets))
	dependencies := make(map[string][]string, len(targets))
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return Graph{}, err
		}
		key := target.Key.String()
		if _, duplicate := byKey[key]; duplicate {
			return Graph{}, fmt.Errorf("duplicate scoped target identity")
		}
		dependencies[key] = nil
		for _, local := range target.Definition.DependsOn {
			dependency := TargetKey{Deployment: deployment, Instance: target.Key.Instance, Target: local}
			target.DependsOn = append(target.DependsOn, dependency)
			dependencies[key] = append(dependencies[key], dependency.String())
		}
		// Dependency lists are sets, not execution ordering instructions.
		slices.SortFunc(target.DependsOn, func(a, b TargetKey) int {
			return strings.Compare(a.String(), b.String())
		})
		byKey[key] = target
	}
	order, err := topologicalOrder(ctx, dependencies)
	if err != nil {
		return Graph{}, fmt.Errorf("target dependencies: %w", err)
	}
	graph := Graph{Deployment: deployment, Targets: make([]ScopedTarget, 0, len(order))}
	for _, key := range order {
		graph.Targets = append(graph.Targets, byKey[key])
	}
	if err := ctx.Err(); err != nil {
		return Graph{}, err
	}
	return graph, nil
}

// topologicalOrder uses explicit edges rather than callback/registration order.
// It is shared by compiled dependencies and scoped target dependencies.
func topologicalOrder(ctx context.Context, dependencies map[string][]string) ([]string, error) {
	remaining := make(map[string]int, len(dependencies))
	dependents := make(map[string][]string, len(dependencies))
	var ready []string
	for name, required := range dependencies {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining[name] = len(required)
		if len(required) == 0 {
			ready = append(ready, name)
		}
		for _, dependency := range required {
			if _, exists := dependencies[dependency]; !exists {
				return nil, fmt.Errorf("missing dependency %s required by %s", dependency, name)
			}
			dependents[dependency] = append(dependents[dependency], name)
		}
	}
	order := make([]string, 0, len(dependencies))
	for len(ready) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		slices.Sort(ready)
		name := ready[0]
		ready = ready[1:]
		order = append(order, name)
		for _, dependent := range dependents[name] {
			remaining[dependent]--
			if remaining[dependent] == 0 {
				ready = append(ready, dependent)
			}
		}
	}
	if len(order) != len(dependencies) {
		return nil, fmt.Errorf("dependency cycle")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return order, nil
}

func cloneTarget(target orchestration.Target) orchestration.Target {
	target.DependsOn = slices.Clone(target.DependsOn)
	target.Containers = slices.Clone(target.Containers)
	for i := range target.Containers {
		target.Containers[i].Contract.Requirements = slices.Clone(target.Containers[i].Contract.Requirements)
	}
	cloneChecks := func(checks []orchestration.Check) []orchestration.Check {
		checks = slices.Clone(checks)
		for i := range checks {
			checks[i].Access = slices.Clone(checks[i].Access)
			checks[i].OptIns = slices.Clone(checks[i].OptIns)
		}
		return checks
	}
	target.Preconditions = cloneChecks(target.Preconditions)
	target.Postconditions = cloneChecks(target.Postconditions)
	target.Observations = cloneChecks(target.Observations)
	return target
}
