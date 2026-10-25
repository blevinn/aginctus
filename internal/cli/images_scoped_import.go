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

// Only this explicit create-only command may import into a named project.
// Never call the legacy ambient-project image runner for these writes.
var importScopedImage = func(ctx context.Context, scope incusscope.ProjectScope, metadata, rootfs, alias string) error {
	return exec.CommandContext(ctx, "incus", scopedImageImportArgs(scope, metadata, rootfs, alias)...).Run()
}

func scopedImageImportArgs(scope incusscope.ProjectScope, metadata, rootfs, alias string) []string {
	// Crucially, do NOT add --reuse. Incus must reject an existing alias
	// atomically, including one created between our final list and import.
	return []string{
		"--force-local", "--project", scope.Name,
		"image", "import", metadata, rootfs, "--alias", alias,
	}
}

func parseScopedImageImportArgs(args []string) (project, alias, directory string, err error) {
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
	if !validImageName(alias) || directory == "" {
		return "", "", "", fmt.Errorf("invalid image alias or artifact directory")
	}
	return project, alias, directory, nil
}

func readScopedImageAlias(ctx context.Context, scope incusscope.ProjectScope, alias string) (fingerprint string, exists bool, err error) {
	raw, err := inspectScopedImages(ctx, scope)
	if err != nil || len(raw) > maxProjectListBytes {
		return "", false, fmt.Errorf("unable to verify scoped image inventory")
	}
	var images []localImage
	if err := json.Unmarshal(raw, &images); err != nil || images == nil {
		return "", false, fmt.Errorf("invalid scoped image inventory")
	}
	for _, image := range images {
		if image.Fingerprint == "" {
			return "", false, fmt.Errorf("invalid image fingerprint")
		}
		for _, item := range image.Aliases {
			if item.Name != alias {
				continue
			}
			if exists {
				return "", false, fmt.Errorf("ambiguous scoped image alias")
			}
			fingerprint, exists = image.Fingerprint, true
		}
	}
	return fingerprint, exists, nil
}

func verifyScopedImageProject(ctx context.Context, scope incusscope.ProjectScope) error {
	status, err := readLocalProjectAssessment(ctx, scope)
	if err != nil || status != string(incusscope.ProjectCompatible) {
		return fmt.Errorf("local image project ownership and feature policy not verified")
	}
	return nil
}

// A narrowly scoped creation command: never adopts or replaces a preexisting
// alias, and never automatically deletes an uncertain imported image.
// The server's default import behavior (no --reuse) is the final collision
// defense against another writer creating the alias after our checks.
func runScopedImageImport(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	project, alias, dir, err := parseScopedImageImportArgs(args)
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
	release, err := acquireScopedImageMutationLease(scope)
	if err != nil {
		fmt.Fprintln(stderr, "images import: project writer lease unavailable; no import attempted")
		return 1
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err := verifyScopedImageProject(ctx, scope); err != nil {
		fmt.Fprintln(stderr, "images import: local project unverified; no import attempted")
		return 1
	}
	if _, exists, err := readScopedImageAlias(ctx, scope, alias); err != nil || exists {
		fmt.Fprintln(stderr, "images import: alias already exists or inventory unavailable; refusing adoption")
		return 1
	}
	metadata, err := imageArtifact(dir, "metadata", ".tar.xz")
	if err != nil {
		fmt.Fprintln(stderr, "images import: cannot resolve metadata artifact")
		return 1
	}
	rootfs, err := imageArtifact(dir, "rootfs", ".squashfs")
	if err != nil {
		fmt.Fprintln(stderr, "images import: cannot resolve rootfs artifact")
		return 1
	}
	fingerprint, err := imageFingerprint(metadata, rootfs)
	if err != nil {
		fmt.Fprintln(stderr, "images import: cannot fingerprint artifacts")
		return 1
	}
	// Recheck project ownership AND the target alias immediately before
	// writing. No global Incus lock is claimed; server collision behavior
	// protects against an alias created in the remaining race window.
	if err := verifyScopedImageProject(ctx, scope); err != nil {
		fmt.Fprintln(stderr, "images import: local project changed before import; refusing mutation")
		return 1
	}
	if _, exists, err := readScopedImageAlias(ctx, scope, alias); err != nil || exists {
		fmt.Fprintln(stderr, "images import: alias changed or inventory unavailable; refusing mutation")
		return 1
	}
	if err := importScopedImage(ctx, scope, metadata, rootfs, alias); err != nil {
		fmt.Fprintln(stderr, "images import: create-only import refused or failed; inspect the project before retrying")
		return 1
	}
	// A command can complete without the requested alias being usable.
	// Never report success until both project policy and alias fingerprint
	// are independently observed. Never automatically roll back: without
	// a transaction lock, an alias may have been changed by another writer.
	if err := verifyScopedImageProject(ctx, scope); err != nil {
		fmt.Fprintln(stderr, "images import: project not verified after import; inspect manually; no automatic rollback")
		return 1
	}
	got, exists, err := readScopedImageAlias(ctx, scope, alias)
	if err != nil || !exists || got != fingerprint {
		fmt.Fprintln(stderr, "images import: alias fingerprint not verified after import; inspect manually; no automatic rollback")
		return 1
	}
	fmt.Fprintf(stdout, "image %q: created in project %q\n", alias, scope.Name)
	return 0
}
