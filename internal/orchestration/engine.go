package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
)

type ExecuteOptions struct {
	DryRun bool
}

type Driver interface {
	Validate(json.RawMessage) error
	SupportsDryRun() bool
	Execute(context.Context, json.RawMessage, ExecuteOptions) error
}

type StepResult struct {
	ID     string
	Driver string
	DryRun bool
}

type Result struct {
	Steps []StepResult
}

type Engine struct {
	drivers map[string]Driver
}

func NewEngine(drivers map[string]Driver) *Engine {
	copied := make(map[string]Driver, len(drivers))
	for name, driver := range drivers {
		copied[name] = driver
	}
	return &Engine{drivers: copied}
}

func (e *Engine) Validate(plan Plan, options ExecuteOptions) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	for _, step := range plan.Steps {
		driver, ok := e.drivers[step.Driver]
		if !ok || driver == nil {
			return fmt.Errorf("orchestration step %q uses unknown driver %q", step.ID, step.Driver)
		}
		if options.DryRun && !driver.SupportsDryRun() {
			return fmt.Errorf("orchestration step %q driver %q does not support dry-run", step.ID, step.Driver)
		}
		if err := driver.Validate(step.Configuration); err != nil {
			return fmt.Errorf("validate orchestration step %q (%s): %w", step.ID, step.Driver, err)
		}
	}
	return nil
}

func (e *Engine) Execute(ctx context.Context, plan Plan, options ExecuteOptions) (Result, error) {
	if err := e.Validate(plan, options); err != nil {
		return Result{}, err
	}

	result := Result{Steps: make([]StepResult, 0, len(plan.Steps))}
	for _, step := range plan.Steps {
		driver := e.drivers[step.Driver]
		if err := driver.Execute(ctx, step.Configuration, options); err != nil {
			return result, fmt.Errorf("execute orchestration step %q (%s): %w", step.ID, step.Driver, err)
		}
		result.Steps = append(result.Steps, StepResult{
			ID:     step.ID,
			Driver: step.Driver,
			DryRun: options.DryRun,
		})
	}
	return result, nil
}
