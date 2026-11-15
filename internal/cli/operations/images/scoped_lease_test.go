package imageops

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// The contract for cooperative writer locks belongs to the operation that
// owns the abstract Unix socket, including cross-process release behavior.
func TestScopedImageMutationLeaseIdentity(t *testing.T) {
	a, err := incusscope.NewProjectScope("aginctus")
	if err != nil {
		t.Fatal(err)
	}
	b, err := incusscope.NewProjectScope("aginctus-other")
	if err != nil {
		t.Fatal(err)
	}
	first, err := ScopedImageLeaseAddress(a)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ScopedImageLeaseAddress(a)
	if err != nil || first != second || !strings.HasPrefix(first, "@aginctus-image-write-v1-") {
		t.Fatalf("unstable abstract lease identity %q / %q: %v", first, second, err)
	}
	different, err := ScopedImageLeaseAddress(b)
	if err != nil || different == first {
		t.Fatalf("different projects share writer lease address: %q", different)
	}
	if len(first) > 107 {
		t.Fatalf("Unix socket name exceeds sun_path limit: %d", len(first))
	}
	for _, bad := range []string{"", "default", "../escape", "name with spaces"} {
		if _, err := ScopedImageLeaseAddress(incusscope.ProjectScope{Name: bad}); err == nil {
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
	release, err := AcquireScopedImageMutationLease(scope)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if leaked, err := AcquireScopedImageMutationLease(scope); err == nil {
		leaked()
		t.Fatal("concurrent writer acquired same project lease")
	}
	releaseOther, err := AcquireScopedImageMutationLease(other)
	if err != nil {
		t.Fatalf("distinct projects should not conflict: %v", err)
	}
	releaseOther()
	release()
	next, err := AcquireScopedImageMutationLease(scope)
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
	release, err := AcquireScopedImageMutationLease(scope)
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
	if release, err := AcquireScopedImageMutationLease(scope); err == nil {
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
	release, err := AcquireScopedImageMutationLease(scope)
	if err != nil {
		t.Fatalf("lease remained after child exit: %v", err)
	}
	release()
}
