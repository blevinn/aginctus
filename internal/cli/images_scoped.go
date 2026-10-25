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
	switch {
	case len(args) == 3 && args[1] == "--project":
		return args[2], nil
	case len(args) == 2 && strings.HasPrefix(args[1], "--project="):
		return strings.TrimPrefix(args[1], "--project="), nil
	default:
		return "", fmt.Errorf("explicit --project NAME is required")
	}
}

// runConfiguredImages leaves the v0.1 ambient-project commands unchanged.
// Only the opt-in list/ls --project command reads the v0.2 project.
// The active image update/rollback algorithm must be scoped and guarded
// separately before user-owned mutations are enabled.
func runConfiguredImages(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
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
