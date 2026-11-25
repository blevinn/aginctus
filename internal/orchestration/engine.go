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
	// Steps includes operations that finished the step, including simulation or
	// steps with no required postconditions. It does not imply runtime readiness.
	Steps []StepResult
	// Operations also retains completed or simulated driver calls when postflight failed.
	// This is not a durable journal or proof that canceled writes did not occur.
	Operations []StepResult
	Conditions []ConditionResult
	Report     *report.Report
	Native     *NativeResult `json:"native,omitempty"`
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
	if err := e.validateConditions(plan, options, false); err != nil {
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

func (e *Engine) Execute(ctx context.Context, plan Plan, options ExecuteOptions) (Result, error) {
	return e.execute(ctx, plan, options, Result{}, executionHooks{
		validate: func(ctx context.Context) error { return e.ValidateContext(ctx, plan, options) },
		operation: func(ctx context.Context, step Step, options ExecuteOptions) error {
			return e.drivers[step.Driver].Execute(ctx, step.Configuration, options)
		},
	})
}

// executionHooks vary data/effect capabilities, not the scheduling or check loop.
type executionHooks struct {
	validate  func(context.Context) error
	authorize func(context.Context) error
	preflight func(context.Context, Step) error
	operation func(context.Context, Step, ExecuteOptions) error
	native    bool
}

func (e *Engine) execute(ctx context.Context, plan Plan, options ExecuteOptions, initial Result, hooks executionHooks) (result Result, err error) {
	result = initial
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
	if err := hooks.validate(ctx); err != nil {
		return result, err
	}
	result.phaseFinished()

	result.Steps = make([]StepResult, 0, len(plan.Steps))
	result.phaseStarted(report.Authorization)
	if hooks.authorize != nil {
		if err := hooks.authorize(ctx); err != nil {
			return result, err
		}
	}
	nativeDryRun := hooks.native && options.DryRun
	if err := e.authorizeConditions(ctx, plan, nativeDryRun); err != nil {
		return result, err
	}
	result.phaseFinished()
	result.phaseStarted(report.Preflight)
	for _, step := range plan.Steps {
		if hooks.preflight != nil {
			if err := hooks.preflight(ctx, step); err != nil {
				return result, err
			}
		}
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
		if !nativeDryRun {
			if err := e.runConditions(ctx, step, PreconditionPhase, &result); err != nil {
				return result, err
			}
		}
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("orchestration canceled before step %q: %w", step.ID, err)
		}
		result.operationStarted()
		firstEffect := 0
		if result.Native != nil {
			firstEffect = len(result.Native.Effects)
		}
		if err := hooks.operation(ctx, step, options); err != nil {
			result.nativeOperationFailed(ctx, firstEffect, err)
			return result, fmt.Errorf("execute orchestration step %q (%s): %w", step.ID, step.Driver, err)
		}
		// A driver may finish normally even if its context was canceled during
		// a blocking native operation. Do not report that step as successful.
		if err := ctx.Err(); err != nil {
			result.nativeOperationFailed(ctx, firstEffect, err)
			return result, fmt.Errorf("orchestration canceled during step %q (%s): %w", step.ID, step.Driver, err)
		}
		result.operationFinished()
		completed := StepResult{
			ID:     step.ID,
			Driver: step.Driver,
			DryRun: options.DryRun,
		}
		if !nativeDryRun {
			result.Operations = append(result.Operations, completed)
		}
		if nativeDryRun {
			if result.reporting != nil {
				result.reporting.verification(report.Simulated, nil)
			}
		} else {
			if err := e.runConditions(ctx, step, PostconditionPhase, &result); err != nil {
				return result, err
			}
		}
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("orchestration canceled during postflight for step %q: %w", step.ID, err)
		}
		result.Steps = append(result.Steps, completed)
		result.stepFinished()
	}
	return result, nil
}
