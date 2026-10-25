package cli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

func TestScopedImageMutationLeaseIdentity(t *testing.T) {
	a, err := incusscope.NewProjectScope("aginctus")
	if err != nil {
		t.Fatal(err)
	}
	b, err := incusscope.NewProjectScope("aginctus-other")
	if err != nil {
		t.Fatal(err)
	}
	first, err := scopedImageLeaseAddress(a)
	if err != nil {
		t.Fatal(err)
	}
	second, err := scopedImageLeaseAddress(a)
	if err != nil || first != second || !strings.HasPrefix(first, "@aginctus-image-write-v1-") {
		t.Fatalf("unstable abstract lease identity %q / %q: %v", first, second, err)
	}
	different, err := scopedImageLeaseAddress(b)
	if err != nil || different == first {
		t.Fatalf("different projects share writer lease address: %q", different)
	}
	if len(first) > 107 {
		t.Fatalf("Unix socket name exceeds sun_path limit: %d", len(first))
	}
	for _, bad := range []string{"", "default", "../escape", "name with spaces"} {
		if _, err := scopedImageLeaseAddress(incusscope.ProjectScope{Name: bad}); err == nil {
			t.Errorf("accepted invalid project scope %q", bad)
		}
	}
}

func TestScopedImageMutationLeaseExclusiveAndReleased(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("abstract Unix sockets require Linux")
	}
	scope, _ := incusscope.NewProjectScope("aginctus-test-lease")
	other, _ := incusscope.NewProjectScope("aginctus-test-lease-other")
	release, err := acquireScopedImageMutationLease(scope)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if leaked, err := acquireScopedImageMutationLease(scope); err == nil {
		leaked()
		t.Fatal("concurrent writer acquired same project lease")
	}
	releaseOther, err := acquireScopedImageMutationLease(other)
	if err != nil {
		t.Fatalf("distinct projects should not conflict: %v", err)
	}
	releaseOther()
	release()
	next, err := acquireScopedImageMutationLease(scope)
	if err != nil {
		t.Fatalf("lease not available after closing listener: %v", err)
	}
	next()
}

func TestScopedImageLeaseSubprocessHelper(t *testing.T) {
	if os.Getenv("AGINCTUS_IMAGE_LEASE_CHILD") != "1" {
		return
	}
	scope, err := incusscope.NewProjectScope("aginctus-test-child-lease")
	if err != nil {
		t.Fatal(err)
	}
	release, err := acquireScopedImageMutationLease(scope)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	fmt.Println("LEASE_READY")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

func TestScopedImageMutationLeaseAcrossProcesses(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("abstract Unix sockets require Linux")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestScopedImageLeaseSubprocessHelper$")
	child.Env = append(os.Environ(), "AGINCTUS_IMAGE_LEASE_CHILD=1")
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stdin.Close()
		_ = child.Process.Kill()
		_ = child.Wait()
	}()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "LEASE_READY" {
		t.Fatalf("child lease not ready: %q %v", line, err)
	}
	scope, _ := incusscope.NewProjectScope("aginctus-test-child-lease")
	if release, err := acquireScopedImageMutationLease(scope); err == nil {
		release()
		t.Fatal("another process acquired a held project lease")
	}
	// Close the child via stdin and reap it before acquiring again.
	if _, err := io.WriteString(stdin, "release\n"); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err != nil {
		t.Fatalf("child did not release lease cleanly: %v", err)
	}
	release, err := acquireScopedImageMutationLease(scope)
	if err != nil {
		t.Fatalf("lease remained after child exit: %v", err)
	}
	release()
}

func TestScopedImageImportLeaseContentionFailsBeforeIncus(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("abstract Unix sockets require Linux")
	}
	cfg, err := (&config.Loader{Environment: nil}).Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := cfg.IncusProjectScope()
	if err != nil {
		t.Fatal(err)
	}
	release, err := acquireScopedImageMutationLease(scope)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	previousProject, previousImages := inspectLocalProjects, inspectScopedImages
	previousImport, previousOwnedImport := importScopedImage, importOwnedScopedImage
	t.Cleanup(func() {
		inspectLocalProjects, inspectScopedImages = previousProject, previousImages
		importScopedImage, importOwnedScopedImage = previousImport, previousOwnedImport
	})
	inspectLocalProjects = func(context.Context) ([]byte, error) {
		t.Fatal("lease contention must fail before project inspection")
		return nil, nil
	}
	inspectScopedImages = func(context.Context, incusscope.ProjectScope) ([]byte, error) {
		t.Fatal("lease contention must fail before image inspection")
		return nil, nil
	}
	importScopedImage = func(context.Context, incusscope.ProjectScope, string, string, string) error {
		t.Fatal("lease contention must fail before image import")
		return nil
	}
	importOwnedScopedImage = func(context.Context, incusscope.ProjectScope, string, string, string, string) error {
		t.Fatal("lease contention must fail before owned image import")
		return nil
	}
	for _, args := range [][]string{
		{"import", "--project", scope.Name, "test-lease", "/nonexistent", "--confirm-create"},
		{"import-owned", "--project", scope.Name, "--owner", "system", "test-lease", "/nonexistent", "--confirm-create"},
	} {
		var out, errOut bytes.Buffer
		if code := runConfiguredImages(context.Background(), args, &out, &errOut, cfg); code != 1 || out.Len() != 0 ||
			!strings.Contains(errOut.String(), "writer lease unavailable") {
			t.Fatalf("contending import %q: code=%d stdout=%q stderr=%q", args, code, out.String(), errOut.String())
		}
	}
}

func TestScopedImageWriterLeaseReleasedOnInspectionFailure(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("abstract Unix sockets require Linux")
	}
	cfg, err := (&config.Loader{Environment: nil}).Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := cfg.IncusProjectScope()
	previousProjects := inspectLocalProjects
	t.Cleanup(func() { inspectLocalProjects = previousProjects })
	inspectLocalProjects = func(context.Context) ([]byte, error) {
		if release, err := acquireScopedImageMutationLease(scope); err == nil {
			release()
			t.Fatal("project inspection did not hold the lease")
		}
		return []byte("[]"), nil
	}
	for _, args := range [][]string{
		{"import", "--project", scope.Name, "test-lease", "/nonexistent", "--confirm-create"},
		{"import-owned", "--project", scope.Name, "--owner", "system", "test-lease", "/nonexistent", "--confirm-create"},
	} {
		var stdout, stderr bytes.Buffer
		if got := runConfiguredImages(context.Background(), args, &stdout, &stderr, cfg); got != 1 || stdout.Len() != 0 {
			t.Fatalf("expected fail-closed project rejection, got %d: %q", got, stdout.String())
		}
		release, err := acquireScopedImageMutationLease(scope)
		if err != nil {
			t.Fatalf("failed import retained project lease: %v", err)
		}
		release()
	}
}

func TestScopedImageWriterLeaseHeldThroughPostcondition(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("abstract Unix sockets require Linux")
	}
	dir, fingerprint := scopedImportFixture(t)
	cfg, err := (&config.Loader{Environment: nil}).Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := cfg.IncusProjectScope()
	for _, owned := range []bool{false, true} {
		t.Run(fmt.Sprintf("owned=%t", owned), func(t *testing.T) {
			beforeProjects, beforeImages := inspectLocalProjects, inspectScopedImages
			beforePlain, beforeOwned := importScopedImage, importOwnedScopedImage
			t.Cleanup(func() {
				inspectLocalProjects, inspectScopedImages = beforeProjects, beforeImages
				importScopedImage, importOwnedScopedImage = beforePlain, beforeOwned
			})
			ensureHeld := func() {
				t.Helper()
				if closeLease, err := acquireScopedImageMutationLease(scope); err == nil {
					closeLease()
					t.Fatal("project lease not held through scoped operation")
				}
			}
			inspectLocalProjects = func(context.Context) ([]byte, error) {
				ensureHeld()
				return []byte(auditOwnedProject), nil
			}
			reads := 0
			inspectScopedImages = func(_ context.Context, project incusscope.ProjectScope) ([]byte, error) {
				ensureHeld()
				if project.Name != scope.Name {
					t.Fatalf("wrong image project: %q", project.Name)
				}
				reads++
				if reads < 3 {
					return []byte("[]"), nil
				}
				if owned {
					return ownedImageFixture(fingerprint, []string{"test-lease"}, expectedImageOwnerClaim(scope, "test-lease", "system")), nil
				}
				return ownedImageFixture(fingerprint, []string{"test-lease"}, nil), nil
			}
			importScopedImage = func(context.Context, incusscope.ProjectScope, string, string, string) error {
				ensureHeld()
				return nil
			}
			importOwnedScopedImage = func(context.Context, incusscope.ProjectScope, string, string, string, string) error {
				ensureHeld()
				return nil
			}
			args := []string{"import", "--project", scope.Name, "test-lease", dir, "--confirm-create"}
			if owned {
				args = []string{"import-owned", "--project", scope.Name, "--owner", "system", "test-lease", dir, "--confirm-create"}
			}
			var stdout, stderr bytes.Buffer
			if code := runConfiguredImages(context.Background(), args, &stdout, &stderr, cfg); code != 0 || reads != 3 {
				t.Fatalf("failed guarded import: code=%d reads=%d stdout=%q stderr=%q", code, reads, stdout.String(), stderr.String())
			}
			followup, err := acquireScopedImageMutationLease(scope)
			if err != nil {
				t.Fatalf("project lease not released after success: %v", err)
			}
			followup()
		})
	}
}
