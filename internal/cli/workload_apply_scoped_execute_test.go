package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

func TestConfirmedScopedApplyBoundaries(t *testing.T) {
	for _, mode := range []string{"success", "missing", "controller", "controller race", "lease", "partial", "postflight"} {
		t.Run(mode, func(t *testing.T) {
			loader := scopedStartLoader()
			state, _, _, units, _ := stubScopedStartEnvironment(t)
			old := ensureScopedExistingWorkload
			t.Cleanup(func() { ensureScopedExistingWorkload = old })
			calls := 0
			ensureScopedExistingWorkload = func(ctx context.Context, intent native.InstanceIntent, guard func(context.Context) error) (native.InstanceEnsureResult, error) {
				if err := guard(ctx); err != nil {
					return native.InstanceEnsureResult{}, err
				}
				calls++
				if intent.Identity.Project != "aginctus" || intent.Identity.Name != "aginctus-dev" || intent.WorkloadID != "dev" || !intent.EnsureRunning || len(intent.Devices) != 2 {
					t.Fatal("unexpected selected intent")
				}
				if mode == "partial" {
					return native.InstanceEnsureResult{Updated: true}, errors.New("sensitive daemon config")
				}
				if mode != "postflight" {
					*state = "Running"
				}
				return native.InstanceEnsureResult{Verified: true}, nil
			}
			switch mode {
			case "missing":
				inspectScopedInstances = func(context.Context, incusscope.ProjectScope) ([]byte, error) { return []byte("[]"), nil }
			case "controller", "controller race":
				original := inspectOperatorUnit
				inspectOperatorUnit = func(ctx context.Context, manager, unit string) ([]byte, error) {
					if mode == "controller" || *units >= len(operatorLifecycleUnits)*2 {
						return []byte("LoadState=loaded\nActiveState=active\nUnitFileState=enabled\n"), nil
					}
					return original(ctx, manager, unit)
				}
			case "lease":
				acquireScopedImageMutationLease = func(incusscope.ProjectScope) (func(), error) { return nil, errors.New("lease denied") }
			}
			code, output, diagnostic := renderScopedApply(t, loader, "workload", "apply", "--project", "aginctus", "dev", "--confirm-apply")
			if mode == "success" {
				if code != 0 || calls != 1 || !strings.Contains(output, "reconciled existing") || diagnostic != "" {
					t.Fatalf("apply failed: %d %q %q", code, output, diagnostic)
				}
			} else {
				if code == 0 || output != "" || strings.Contains(diagnostic, "sensitive") {
					t.Fatalf("unsafe success/disclosure: %d %q %q", code, output, diagnostic)
				}
				if mode != "partial" && mode != "postflight" && calls != 0 {
					t.Fatal("authority boundary crossed")
				}
			}
		})
	}
}

func TestScopedApplyConfirmationSyntax(t *testing.T) {
	loader := scopedStartLoader()
	for _, args := range [][]string{
		{"workload", "apply", "dev", "--confirm-apply"},
		{"workload", "apply", "--project", "aginctus", "dev"},
		{"workload", "apply", "--project", "aginctus", "dev", "--confirm-apply", "--dry-run"},
		{"workload", "apply", "--project", "aginctus", "dev", "--confirm-apply", "--render"},
		{"workload", "apply", "--project", "aginctus", "dev", "--confirm-apply", "--force"},
	} {
		code, output, _ := renderScopedApply(t, loader, args...)
		if code == 0 || output != "" {
			t.Fatalf("accepted unsafe syntax %q", args)
		}
	}
}
