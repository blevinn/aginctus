package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// A scoped read always uses the local daemon and a named project. Never
// dispatch through the legacy ambient-project image runner.
var inspectScopedImages = func(ctx context.Context, scope incusscope.ProjectScope) ([]byte, error) {
	return exec.CommandContext(ctx, "incus", scopedImageListArgs(scope)...).Output()
}

func scopedImageListArgs(scope incusscope.ProjectScope) []string {
	return []string{"--force-local", "--project", scope.Name, "image", "list", "--format", "json"}
}

func parseScopedImageListArgs(args []string) (string, error) {
	var name string
	switch {
	case len(args) == 3 && args[1] == "--project":
		name = args[2]
	case len(args) == 2 && strings.HasPrefix(args[1], "--project="):
		name = strings.TrimPrefix(args[1], "--project=")
	default:
		return "", fmt.Errorf("explicit --project NAME is required")
	}
	if _, err := incusscope.NewProjectScope(name); err != nil {
		return "", err
	}
	return name, nil
}

// runConfiguredImages keeps existing v0.1 image list/update untouched.
// Opt-in project inventory is read-only; the separate explicitly confirmed
// scoped import is create-only and cannot replace existing image aliases.
func runConfiguredImages(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	if len(args) > 0 && args[0] == "replace-plan" {
		return runImageReplacePlan(ctx, args, stdout, stderr, cfg)
	}
	if len(args) > 0 && args[0] == "import-owned" {
		return runScopedOwnedImageImport(ctx, args, stdout, stderr, cfg)
	}
	if len(args) > 0 && args[0] == "owner-audit" {
		return runImageOwnerAudit(ctx, args, stdout, stderr, cfg)
	}
	if len(args) > 0 && args[0] == "import" {
		return runScopedImageImport(ctx, args, stdout, stderr, cfg)
	}
	if len(args) < 2 || (args[0] != "list" && args[0] != "ls") {
		return runImages(ctx, args, stdout, stderr)
	}
	name, err := parseScopedImageListArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, "Usage: aginctus images list|ls --project NAME")
		return 2
	}
	scope, err := cfg.IncusProjectScope()
	if err != nil || name != scope.Name {
		fmt.Fprintln(stderr, "images list: explicit project must match the validated incus.project configuration")
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	status, err := readLocalProjectAssessment(ctx, scope)
	if err != nil || status != string(incusscope.ProjectCompatible) {
		fmt.Fprintln(stderr, "images list: local Incus project ownership or feature policy not verified")
		return 1
	}
	output, err := inspectScopedImages(ctx, scope)
	if err != nil || len(output) > maxProjectListBytes {
		fmt.Fprintln(stderr, "images list: scoped image inventory unavailable")
		return 1
	}
	var images []localImage
	if err := json.Unmarshal(output, &images); err != nil || images == nil {
		fmt.Fprintln(stderr, "images list: malformed scoped image inventory")
		return 1
	}
	printLocalImages(stdout, images)
	return 0
}
