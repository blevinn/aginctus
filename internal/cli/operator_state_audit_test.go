package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeGatewayState(t *testing.T, root, id string) string {
	t.Helper()
	dir := filepath.Join(root, "aginctus", "gateway", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(dir, "secrets.json")
	if err := os.WriteFile(secret, []byte("sensitive-secret-must-never-be-printed"), 0o600); err != nil {
		t.Fatal(err)
	}
	return secret
}

func TestOperatorGatewayStateMetadata(t *testing.T) {
	uid := uint32(os.Geteuid())
	t.Run("absent is not conflated with state initialized", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "missing")
		if actual := inspectGatewayState(root, "local", uid); actual != gatewayStateAbsent {
			t.Fatalf("inspection=%q", actual)
		}
	})
	t.Run("private credential file metadata", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "state")
		secret := makeGatewayState(t, root, "local")
		if actual := inspectGatewayState(root, "local", uid); actual != gatewayStatePresent {
			t.Fatalf("inspection=%q", actual)
		}
		if err := os.Remove(secret); err != nil {
			t.Fatal(err)
		}
		if actual := inspectGatewayState(root, "local", uid); actual != gatewayStateIncomplete {
			t.Fatalf("missing secrets with state directory=%q", actual)
		}
	})
	t.Run("unsafe permissions", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "state")
		secret := makeGatewayState(t, root, "local")
		if err := os.Chmod(secret, 0o644); err != nil {
			t.Fatal(err)
		}
		if actual := inspectGatewayState(root, "local", uid); actual != gatewayStateUnsafe {
			t.Fatalf("world-readable secret=%q", actual)
		}
		if err := os.Chmod(secret, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Dir(secret), 0o750); err != nil {
			t.Fatal(err)
		}
		if actual := inspectGatewayState(root, "local", uid); actual != gatewayStateUnsafe {
			t.Fatalf("shared gateway directory=%q", actual)
		}
	})
	t.Run("foreign owner", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "state")
		makeGatewayState(t, root, "local")
		if actual := inspectGatewayState(root, "local", uid+1); actual != gatewayStateUnsafe {
			t.Fatalf("foreign owner=%q", actual)
		}
	})
	t.Run("symlinked secret and state tree", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "state")
		secret := makeGatewayState(t, root, "local")
		if err := os.Remove(secret); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(t.TempDir(), "nonexistent-target"), secret); err != nil {
			t.Fatal(err)
		}
		if actual := inspectGatewayState(root, "local", uid); actual != gatewayStateUnsafe {
			t.Fatalf("symlinked secret=%q", actual)
		}
		alias := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(root, alias); err != nil {
			t.Fatal(err)
		}
		if actual := inspectGatewayState(alias, "local", uid); actual != gatewayStateUnsafe {
			t.Fatalf("symlinked root=%q", actual)
		}
	})
	t.Run("malformed gateway ID", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "state")
		for _, id := range []string{"", "../other", "a/b", "bad space"} {
			if actual := inspectGatewayState(root, id, uid); actual != gatewayStateUnsafe {
				t.Errorf("id=%q inspection=%q", id, actual)
			}
		}
	})
}

func TestOperatorGatewayCredentialLocationIsNotAnOwnerHandoff(t *testing.T) {
	uid := uint32(os.Geteuid())
	root := filepath.Join(t.TempDir(), "state")
	makeGatewayState(t, root, "local")
	var out, errs bytes.Buffer
	code := renderOperatorStateAudit("user", root, "local", uid, &out, &errs)
	if code != 0 || !strings.Contains(out.String(), "User gateway credential metadata: present") ||
		!strings.Contains(out.String(), "neither running service nor Incus project authority verified") {
		t.Fatalf("incorrect location report code=%d stdout=%q stderr=%q", code, out.String(), errs.String())
	}
	if strings.Contains(out.String()+errs.String(), "sensitive-secret") || strings.Contains(out.String()+errs.String(), root) {
		t.Fatal("credential diagnostic disclosed contents or private path")
	}
	out.Reset()
	errs.Reset()
	if code := renderOperatorStateAudit("unknown", root, "local", uid, &out, &errs); code != 2 {
		t.Fatal("unknown credential location accepted")
	}
}

func TestOperatorGatewayCredentialAuditRejectsUnsafeLocation(t *testing.T) {
	uid := uint32(os.Geteuid())
	root := filepath.Join(t.TempDir(), "state")
	secret := makeGatewayState(t, root, "local")
	if err := os.Chmod(secret, 0o644); err != nil { t.Fatal(err) }
	var out, errs bytes.Buffer
	code := renderOperatorStateAudit("user", root, "local", uid, &out, &errs)
	if code != 1 || !strings.Contains(errs.String(), "unsafe or unverified") {
		t.Fatalf("unsafe credential location accepted code=%d out=%q err=%q", code, out.String(), errs.String())
	}
	if strings.Contains(out.String()+errs.String(), "sensitive-secret") || strings.Contains(out.String()+errs.String(), root) {
		t.Fatal("credential diagnostic leaked data or paths")
	}
}
