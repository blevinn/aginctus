package herdrremote

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/orchestration"
	sshaccess "github.com/blevinn/aginctus/internal/orchestration/drivers/sshaccess"
	"github.com/blevinn/aginctus/internal/workload"
)

type Spec struct {
	WorkloadID     string
	ClientInstance string
	TargetInstance string
	TargetAccount  string
}

type Options struct {
	DryRun    bool
	Operation sshaccess.Operation
}

func FromConfig(effective *config.Config, workloadID string) (Spec, error) {
	client, err := herdrclient.FromConfig(effective)
	if err != nil {
		return Spec{}, err
	}
	target, err := workload.FromConfig(effective, workloadID)
	if err != nil {
		return Spec{}, err
	}

	account := ""
	switch target.Runtime {
	case "opencode":
		account = "agent"
	default:
		return Spec{}, fmt.Errorf("workload %q runtime %q has no Herdr remote login contract", workloadID, target.Runtime)
	}

	return Spec{
		WorkloadID:     target.ID,
		ClientInstance: client.Name,
		TargetInstance: target.Name,
		TargetAccount:  account,
	}, nil
}

func (s Spec) Plan(operation sshaccess.Operation) (orchestration.Plan, error) {
	cfg := sshaccess.Config{
		Operation:      operation,
		ClientInstance: s.ClientInstance,
		WorkloadID:     s.WorkloadID,
		TargetInstance: s.TargetInstance,
		TargetAccount:  s.TargetAccount,
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return orchestration.Plan{}, fmt.Errorf("encode Herdr remote plan: %w", err)
	}
	plan := orchestration.Plan{
		APIVersion: orchestration.APIVersion,
		Kind:       orchestration.Kind,
		Metadata:   orchestration.Metadata{Name: "herdr-remote-" + s.WorkloadID},
		Steps: []orchestration.Step{{
			ID:            "herdr-remote-" + s.WorkloadID,
			Driver:        "ssh-access",
			Configuration: raw,
		}},
	}
	if err := plan.Validate(); err != nil {
		return orchestration.Plan{}, err
	}
	return plan, nil
}

func (s Spec) Execute(ctx context.Context, runtime sshaccess.Runtime, options Options) error {
	plan, err := s.Plan(options.Operation)
	if err != nil {
		return err
	}
	engine := orchestration.NewEngine(map[string]orchestration.Driver{
		"ssh-access": sshaccess.New(runtime),
	})
	if _, err := engine.Execute(ctx, plan, orchestration.ExecuteOptions{DryRun: options.DryRun}); err != nil {
		return fmt.Errorf("Herdr remote %q: %w", s.WorkloadID, err)
	}
	return nil
}
