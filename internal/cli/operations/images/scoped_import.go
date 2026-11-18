package imageops

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
	incusimages "github.com/blevinn/aginctus/internal/incus/images"
)

// ScopedImportActions injects the exact authorization, local inventory,
// cooperative lease and create-only import capabilities used by this command.
type ScopedImportActions struct {
	Lease func(incusscope.ProjectScope) (func(), error)
	Verify func(context.Context, incusscope.ProjectScope) error
	ReadAlias func(context.Context, incusscope.ProjectScope, string) (string, bool, error)
	Artifact func(string, string, string) (string, error)
	Fingerprint func(string, string) (string, error)
	Import func(context.Context, incusscope.ProjectScope, string, string, string) error
}

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
	// Hold one project-wide cooperative writer lease through the final
	// post-import observation, not merely the import subprocess.
	release, err := actions.Lease(scope)
	if err != nil {
		fmt.Fprintln(stderr, "images import: project writer lease unavailable; no import attempted")
		return 1
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err := actions.Verify(ctx, scope); err != nil {
		fmt.Fprintln(stderr, "images import: local project unverified; no import attempted")
		return 1
	}
	if _, exists, err := actions.ReadAlias(ctx, scope, alias); err != nil || exists {
		fmt.Fprintln(stderr, "images import: alias already exists or inventory unavailable; refusing adoption")
		return 1
	}
	metadata, err := actions.Artifact(dir, "metadata", ".tar.xz")
	if err != nil {
		fmt.Fprintln(stderr, "images import: cannot resolve metadata artifact")
		return 1
	}
	rootfs, err := actions.Artifact(dir, "rootfs", ".squashfs")
	if err != nil {
		fmt.Fprintln(stderr, "images import: cannot resolve rootfs artifact")
		return 1
	}
	fingerprint, err := actions.Fingerprint(metadata, rootfs)
	if err != nil {
		fmt.Fprintln(stderr, "images import: cannot fingerprint artifacts")
		return 1
	}
	// Recheck project ownership AND the target alias immediately before
	// writing. No global Incus lock is claimed; server collision behavior
	// protects against an alias created in the remaining race window.
	if err := actions.Verify(ctx, scope); err != nil {
		fmt.Fprintln(stderr, "images import: local project changed before import; refusing mutation")
		return 1
	}
	if _, exists, err := actions.ReadAlias(ctx, scope, alias); err != nil || exists {
		fmt.Fprintln(stderr, "images import: alias changed or inventory unavailable; refusing mutation")
		return 1
	}
	if err := actions.Import(ctx, scope, metadata, rootfs, alias); err != nil {
		fmt.Fprintln(stderr, "images import: create-only import refused or failed; inspect the project before retrying")
		return 1
	}
	// A command can complete without the requested alias being usable.
	// Never report success until both project policy and alias fingerprint
	// are independently observed. Never automatically roll back: without
	// a transaction lock, an alias may have been changed by another writer.
	if err := actions.Verify(ctx, scope); err != nil {
		fmt.Fprintln(stderr, "images import: project not verified after import; inspect manually; no automatic rollback")
		return 1
	}
	got, exists, err := actions.ReadAlias(ctx, scope, alias)
	if err != nil || !exists || got != fingerprint {
		fmt.Fprintln(stderr, "images import: alias fingerprint not verified after import; inspect manually; no automatic rollback")
		return 1
	}
	fmt.Fprintf(stdout, "image %q: created in project %q\n", alias, scope.Name)
	return 0
}
