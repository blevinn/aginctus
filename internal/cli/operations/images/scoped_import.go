package imageops

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
	incusimages "github.com/blevinn/aginctus/internal/incus/images"
)

// ScopedImportActions injects the exact authorization, local inventory,
// cooperative lease and create-only import capabilities used by this command.
type ScopedImportActions = incusimages.CreateActions

func ScopedImageImportArgs(scope incusscope.ProjectScope, metadata, rootfs, alias string) []string {
	// Crucially, do NOT add --reuse. Incus must reject an existing alias
	// atomically, including one created between our final list and import.
	return []string{
		"--force-local", "--project", scope.Name,
		"image", "import", metadata, rootfs, "--alias", alias,
	}
}

func ParseScopedImageImportArgs(args []string) (project, alias, directory string, err error) {
	switch {
	case len(args) == 6 && args[0] == "import" && args[1] == "--project" && args[5] == "--confirm-create":
		project, alias, directory = args[2], args[3], args[4]
	case len(args) == 5 && args[0] == "import" && strings.HasPrefix(args[1], "--project=") && args[4] == "--confirm-create":
		project, alias, directory = strings.TrimPrefix(args[1], "--project="), args[2], args[3]
	default:
		return "", "", "", fmt.Errorf("explicit project, image alias, artifact directory and --confirm-create required")
	}
	if _, err = incusscope.NewProjectScope(project); err != nil {
		return "", "", "", err
	}
	if !ValidImageName(alias) || directory == "" {
		return "", "", "", fmt.Errorf("invalid image alias or artifact directory")
	}
	return project, alias, directory, nil
}

// Compatibility adapters preserve the CLI call contract while Incus owns
// inventory ambiguity and verified project-policy semantics.
func ReadScopedImageAlias(ctx context.Context, scope incusscope.ProjectScope, alias string, inspect func(context.Context, incusscope.ProjectScope) ([]byte, error), maxBytes int) (string, bool, error) {
	return incusimages.ScopedAliasFingerprint(ctx, scope, alias, inspect, maxBytes)
}

func VerifyScopedImageProject(ctx context.Context, scope incusscope.ProjectScope, read func(context.Context, incusscope.ProjectScope) (string, error)) error {
	return incusimages.VerifyScopedImageProject(ctx, scope, read)
}

// A narrowly scoped creation command: never adopts or replaces a preexisting
// alias, and never automatically deletes an uncertain imported image.
// The server's default import behavior (no --reuse) is the final collision
// defense against another writer creating the alias after our checks.
func RunScopedImageImport(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config, actions ScopedImportActions) int {
	project, alias, dir, err := ParseScopedImageImportArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, "Usage: aginctus images import --project NAME ALIAS PATH --confirm-create")
		return 2
	}
	scope, err := cfg.IncusProjectScope()
	if err != nil || project != scope.Name {
		fmt.Fprintln(stderr, "images import: project must match the declared incus.project setting")
		return 2
	}
	if err := incusimages.CreateScopedImage(ctx, scope, alias, dir, actions); err != nil {
		fmt.Fprintf(stderr, "images import: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "image %q: created in project %q\n", alias, scope.Name)
	return 0
}
