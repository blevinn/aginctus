package imageops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

func ScopedImageListArgs(scope incusscope.ProjectScope) []string {
	return []string{"--force-local", "--project", scope.Name, "image", "list", "--format", "json"}
}

func ParseScopedImageListArgs(args []string) (string, error) {
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

// ListScopedImages accepts only project-local verified inventory input and
// never attempts a remote/default-project fallback on invalid visibility.
func ListScopedImages(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config,
	readProject func(context.Context, incusscope.ProjectScope) (string, error),
	inspect func(context.Context, incusscope.ProjectScope) ([]byte, error),
	maxBytes int,
) int {
	name, err := ParseScopedImageListArgs(args)
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
	status, err := readProject(ctx, scope)
	if err != nil || status != string(incusscope.ProjectCompatible) {
		fmt.Fprintln(stderr, "images list: local Incus project ownership or feature policy not verified")
		return 1
	}
	output, err := inspect(ctx, scope)
	if err != nil || len(output) > maxBytes {
		fmt.Fprintln(stderr, "images list: scoped image inventory unavailable")
		return 1
	}
	var images []LocalImage
	if err := json.Unmarshal(output, &images); err != nil || images == nil {
		fmt.Fprintln(stderr, "images list: malformed scoped image inventory")
		return 1
	}
	PrintLocalImages(stdout, images)
	return 0
}
