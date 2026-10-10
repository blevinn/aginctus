package compose

import (
	"context"
	"fmt"
	composeclient "github.com/lxc/incus-compose/client"
	"sort"
	"strings"
)

// ComposeRecreationPlan is a non-destructive inventory for replacing a
// drifted instance. It never triggers stop, delete, or create operations.
type ComposeRecreationPlan struct {
	Required          bool
	PersistentDevices []string
}

// planComposeRecreation inventories disk devices that a future instance
// replacement must preserve. Unknown disk layouts fail closed.
func planComposeRecreation(drift *ComposeDriftError, devices map[string]map[string]string) (ComposeRecreationPlan, error) {
	if drift == nil {
		return ComposeRecreationPlan{}, nil
	}
	plan := ComposeRecreationPlan{Required: true}
	for name, values := range devices {
		if values["type"] != "disk" {
			continue
		}
		path := values["path"]
		if path == "" {
			return ComposeRecreationPlan{}, fmt.Errorf("cannot plan recreation: disk %q has no mount path", name)
		}
		if path == "/" {
			continue
		}
		source, pool := values["source"], values["pool"]
		if source == "" || pool == "" || strings.TrimSpace(source) != source || strings.TrimSpace(pool) != pool {
			return ComposeRecreationPlan{}, fmt.Errorf("cannot plan recreation: disk %q lacks an explicit valid volume reference", name)
		}
		plan.PersistentDevices = append(plan.PersistentDevices, name)
	}
	sort.Strings(plan.PersistentDevices)
	return plan, nil
}

func rejectPersistentDisks(existingDevices map[string]map[string]string) error {
	for name, device := range existingDevices {
		if device["type"] == "disk" && device["path"] != "/" {
			return fmt.Errorf("refuse automatic recreation: instance disk %q requires persistent-storage migration", name)
		}
	}
	return nil
}

// recreateWithoutPersistentDisks performs a deliberately restricted replacement
// lifecycle. The caller is responsible for checking exact ownership both during
// preflight and again immediately before invoking it. It never requests volume
// deletion. No action is taken unless no non-root disks are attached.
func recreateWithoutPersistentDisks(ctx context.Context, existingDevices map[string]map[string]string, recheckOwnership, stop, remove, create func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := rejectPersistentDisks(existingDevices); err != nil {
		return err
	}
	if err := recheckOwnership(ctx); err != nil {
		return fmt.Errorf("refuse automatic recreation: ownership recheck: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := stop(ctx); err != nil {
		return fmt.Errorf("stop owned instance for recreation: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := recheckOwnership(ctx); err != nil {
		return fmt.Errorf("refuse instance deletion: ownership changed: %w", err)
	}
	if err := remove(ctx); err != nil {
		return fmt.Errorf("delete owned instance without volumes: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("instance removed; recreation canceled: %w", err)
	}
	if err := create(ctx); err != nil {
		return fmt.Errorf("instance removed; recreation failed and manual recovery may be required: %w", err)
	}
	return nil
}

// verifyComposeRecreationVolumes ensures replacement never silently rebinds
// an existing persistent mount to a different volume or drops it. The
// caller must compare the actual instance devices with the fully materialized
// desired instance devices, including Compose-generated mounts.
func verifyComposeRecreationVolumes(existing, replacement map[string]map[string]string) error {
	for name, current := range existing {
		if current["type"] != "disk" || current["path"] == "/" {
			continue
		}
		if current["path"] == "" || current["pool"] == "" || current["source"] == "" {
			return fmt.Errorf("cannot replace instance: existing disk %q has ambiguous persistent storage", name)
		}
		desired, ok := replacement[name]
		if !ok {
			return fmt.Errorf("cannot replace instance: persistent disk %q would be removed", name)
		}
		for _, field := range []string{"type", "path", "pool", "source"} {
			if desired[field] != current[field] {
				return fmt.Errorf("cannot replace instance: persistent disk %q changes %s", name, field)
			}
		}
	}
	return nil
}

// recreateWithRetainedVolumes checks the fully materialized replacement,
// then executes the already ownership-guarded lifecycle. No volume-delete
// options are passed through this path.
func recreateWithRetainedVolumes(ctx context.Context, oldDevices, replacement map[string]map[string]string, recheckOwnership, stop, remove, create func(context.Context) error) error {
	if err := verifyComposeRecreationVolumes(oldDevices, replacement); err != nil {
		return err
	}
	return recreateWithoutPersistentDisks(ctx, nil, recheckOwnership, stop, remove, create)
}

// materializeComposeDevices converts generated and raw Compose devices to the
// exact Incus device shape before any existing instance is replaced.
func materializeComposeDevices(config composeclient.InstanceConfig) (map[string]map[string]string, error) {
	result := make(map[string]map[string]string, len(config.Devices)+len(config.ExtraDevices))
	for _, dev := range config.Devices {
		name, values, err := dev.ToIncusDevice()
		if err != nil {
			return nil, fmt.Errorf("materialize Compose device: %w", err)
		}
		if _, duplicate := result[name]; duplicate {
			return nil, fmt.Errorf("duplicate Compose device %q", name)
		}
		result[name] = values
	}
	for name, values := range config.ExtraDevices {
		if _, duplicate := result[name]; duplicate {
			return nil, fmt.Errorf("duplicate Compose device %q", name)
		}
		result[name] = values
	}
	return result, nil
}
