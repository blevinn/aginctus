package orchestration

import (
	"context"
	"errors"
	"time"

	"github.com/blevinn/aginctus/internal/orchestration/native"
	check "github.com/blevinn/aginctus/pkg/orchestration"
)

// NativeObservationCheck reports fresh, nonsecret, revision-pinned evidence.
func NativeObservationCheck(target, id, claim, revision string, observe func(context.Context) (string, error)) BoundCheck {
	return BoundCheck{
		Check:            check.Check{ID: id, Claim: claim, Scope: "selected native resource", Effect: check.ReadOnly, Access: []check.Access{check.IncusAccess}, Required: true, Timeout: 30 * time.Second},
		AcceptedRevision: revision,
		Evaluate: func(ctx context.Context) (check.Finding, error) {
			incarnation, err := observe(ctx)
			if err != nil {
				return check.Finding{}, err
			}
			return check.Finding{TargetID: target, CheckID: id, Outcome: check.Passed, Code: "observed", ObservedAt: time.Now(), ResourceIncarnation: incarnation, AcceptedRevision: revision}, nil
		},
	}
}

// BindNativeProjectInstance wires one invocation-local domain stage. verifyUse
// checks independently supplied dependencies at submission, after authority.
func BindNativeProjectInstance(id, revision string, stage native.ProjectInstanceStage, expectedIncarnation time.Time, preflightUse, verifyUse func(context.Context) error) NativeResourceBinding {
	var previewIncarnation, appliedIncarnation time.Time
	var applied bool
	return NativeResourceBinding{
		Validate: func() error {
			if preflightUse == nil || verifyUse == nil {
				return errors.New("native attachment and persistence verification required")
			}
			return nil
		},
		Preview: func(ctx context.Context) (NativePreview, error) {
			actions, incarnation, err := stage.Observe(ctx)
			if err != nil {
				return NativePreview{}, err
			}
			if !expectedIncarnation.IsZero() && !incarnation.Equal(expectedIncarnation) {
				return NativePreview{}, errors.New("instance differs from authorized live incarnation")
			}
			if err := preflightUse(ctx); err != nil {
				return NativePreview{}, err
			}
			previewIncarnation = incarnation
			result := NativePreview{}
			for _, action := range actions {
				result.Actions = append(result.Actions, NativeEffect{Action: action, Target: id})
			}
			return result, nil
		},
		Apply: func(ctx context.Context, run RunNativeEffect) error {
			if err := verifyUse(ctx); err != nil {
				return err
			}
			var err error
			appliedIncarnation, err = stage.Apply(ctx, previewIncarnation, func(action native.Action, submit func(context.Context) error) error {
				return run(NativeEffect{Action: action, Target: id}, func(ctx context.Context) error {
					if err := verifyUse(ctx); err != nil {
						return err
					}
					return submit(ctx)
				})
			})
			applied = err == nil
			return err
		},
		Conditions: StepConditions{Postconditions: []BoundCheck{NativeObservationCheck(id, "instance-converged", "Selected instance lifecycle postcondition verified", revision, func(ctx context.Context) (string, error) {
			if err := verifyUse(ctx); err != nil {
				return "", err
			}
			actions, incarnation, err := stage.Observe(ctx)
			if err != nil || !applied || !incarnation.Equal(appliedIncarnation) || len(actions) != 1 || actions[0] != native.Unchanged {
				return "", errors.New("instance lifecycle postcondition unverified")
			}
			if incarnation.IsZero() {
				return "", nil
			}
			return incarnation.UTC().Format(time.RFC3339Nano), nil
		})}},
	}
}
