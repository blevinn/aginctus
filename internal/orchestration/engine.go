package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	collection "github.com/blevinn/aginctus/internal/observability"
	report "github.com/blevinn/aginctus/pkg/observability"
)

type ExecuteOptions struct {
	DryRun bool
	// Reporting collects a bounded, nonsecret lifecycle trace in Result.Report.
	Reporting bool
	// Monitor enables reporting and exposes live, read-only status to its host.
	// Each handle is single-use; reuse cannot mix independent executions.
	Monitor *collection.Monitor
}

type Driver interface {
	// ValidateContext performs configuration validation and any existing read-only
	// preflight. Blocking work must cooperate with the caller's cancellation.
	ValidateContext(context.Context, json.RawMessage) error
	SupportsDryRun() bool
	Execute(context.Context, json.RawMessage, ExecuteOptions) error
}

type StepResult struct {
	ID     string
	Driver string
	DryRun bool
}

type Result struct {
	// Steps includes only operations whose required postconditions passed.
	Steps []StepResult
	// Operations also retains completed driver calls when postflight failed.
	// This is not a durable journal or proof that canceled writes did not occur.
	Operations []StepResult
	Conditions []ConditionResult
	Report     *report.Report
	reporting  *executionReport
}

type Engine struct {
	drivers     map[string]Driver
	conditions  map[string]StepConditions
	checkPolicy CheckPolicy
}

func NewEngine(drivers map[string]Driver) *Engine {
	copied := make(map[string]Driver, len(drivers))
	for name, driver := range drivers {
		copied[name] = driver
	}
	return &Engine{drivers: copied}
}

func (e *Engine) Validate(plan Plan, options ExecuteOptions) error {
	return e.ValidateContext(context.Background(), plan, options)
}

// ValidateContext validates the complete plan before any step may execute.
// Cancellation stops validation; no partial validation authorizes execution.
func (e *Engine) ValidateContext(ctx context.Context, plan Plan, options ExecuteOptions) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("orchestration canceled before validation: %w", err)
	}
	if err := plan.Validate(); err != nil {
		return err
	}
	if err := e.validateConditions(plan, options); err != nil {
		return err
	}
	for _, step := range plan.Steps {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("orchestration canceled before validating step %q: %w", step.ID, err)
		}
		driver, ok := e.drivers[step.Driver]
		if !ok || driver == nil {
			return fmt.Errorf("orchestration step %q uses unknown driver %q", step.ID, step.Driver)
		}
		if options.DryRun && !driver.SupportsDryRun() {
			return fmt.Errorf("orchestration step %q driver %q does not support dry-run", step.ID, step.Driver)
		}
		// Preserve both the validation cause and cancellation, even when a
		// driver returns nil or an unrelated error after cancellation.
		validationErr := driver.ValidateContext(ctx, step.Configuration)
		if err := errors.Join(validationErr, ctx.Err()); err != nil {
			return fmt.Errorf("validate orchestration step %q (%s): %w", step.ID, step.Driver, err)
		}
	}
	return ctx.Err()
}

func (e *Engine) Execute(ctx context.Context, plan Plan, options ExecuteOptions) (result Result, err error) {
	if options.Reporting || options.Monitor != nil {
		result.reporting = newExecutionReport(plan, options.DryRun, options.Monitor)
		defer func() {
			trace := result.reporting.finish(ctx, err)
			result.Report, result.reporting = &trace, nil
		}()
	}
	// Drivers do not receive the host's monitor-claim capability.
	options.Monitor = nil
	result.phaseStarted(report.Validation)
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("orchestration canceled before validation: %w", err)
	}
	if err := e.ValidateContext(ctx, plan, options); err != nil {
		return result, err
	}
	result.phaseFinished()

	result.Steps = make([]StepResult, 0, len(plan.Steps))
	result.phaseStarted(report.Authorization)
	if err := e.authorizeConditions(ctx, plan); err != nil {
		return result, err
	}
	result.phaseFinished()
	result.phaseStarted(report.Preflight)
	for _, step := range plan.Steps {
		if err := e.runConditions(ctx, step, PreflightPhase, &result); err != nil {
			return result, err
		}
	}
	result.phaseFinished()
	for _, step := range plan.Steps {
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("orchestration canceled before step %q: %w", step.ID, err)
		}
		result.stepStarted(step, e.conditions[step.ID].Scope)
		if err := e.runConditions(ctx, step, PreconditionPhase, &result); err != nil {
			return result, err
		}
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("orchestration canceled before step %q: %w", step.ID, err)
		}
		driver := e.drivers[step.Driver]
		result.operationStarted()
		if err := driver.Execute(ctx, step.Configuration, options); err != nil {
			return result, fmt.Errorf("execute orchestration step %q (%s): %w", step.ID, step.Driver, err)
		}
		// A driver may finish normally even if its context was canceled during
		// a blocking native operation. Do not report that step as successful.
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("orchestration canceled during step %q (%s): %w", step.ID, step.Driver, err)
		}
		result.operationFinished()
		completed := StepResult{
			ID:     step.ID,
			Driver: step.Driver,
			DryRun: options.DryRun,
		}
		result.Operations = append(result.Operations, completed)
		if err := e.runConditions(ctx, step, PostconditionPhase, &result); err != nil {
			return result, err
		}
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("orchestration canceled during postflight for step %q: %w", step.ID, err)
		}
		result.Steps = append(result.Steps, completed)
		result.stepFinished()
	}
	return result, nil
}
