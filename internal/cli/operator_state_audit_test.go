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

func TestOperatorGatewayStateOwnerSelection(t *testing.T) {
	uid := uint32(os.Geteuid())
	dir := t.TempDir()
	system := filepath.Join(dir, "system")
	user := filepath.Join(dir, "user")
	for _, root := range []string{system, user} {
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	check := func(owner string, success bool, sys, usr gatewayStateEvidence) {
		t.Helper()
		actualSystem, actualUser, ok := auditOperatorGatewayState(owner, system, user, "local", uid, uid)
		if ok != success || actualSystem != sys || actualUser != usr {
			t.Errorf("owner=%q got (%s,%s,%v); expected (%s,%s,%v)", owner, actualSystem, actualUser, ok, sys, usr, success)
		}
	}
	check("system", true, gatewayStateAbsent, gatewayStateAbsent)
	makeGatewayState(t, system, "local")
	check("system", true, gatewayStatePresent, gatewayStateAbsent)
	check("user", false, gatewayStatePresent, gatewayStateAbsent)
	makeGatewayState(t, user, "local")
	check("system", false, gatewayStatePresent, gatewayStatePresent)
	check("user", false, gatewayStatePresent, gatewayStatePresent)
	if _, _, ok := auditOperatorGatewayState("system", system, system, "local", uid, uid); ok {
		t.Error("same state root must not pass for two owner roles")
	}
}

func TestOperatorGatewayStateAuditRedactsAndRejectsConflict(t *testing.T) {
	uid := uint32(os.Geteuid())
	dir := t.TempDir()
	system := filepath.Join(dir, "system")
	user := filepath.Join(dir, "user")
	makeGatewayState(t, system, "local")
	makeGatewayState(t, user, "local")
	var out, errOut bytes.Buffer
	code := renderOperatorStateAudit("system", system, user, "local", uid, uid, &out, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "unverified or conflicting") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	allOutput := out.String() + errOut.String()
	if strings.Contains(allOutput, "sensitive-secret") || strings.Contains(allOutput, "secrets.json") || strings.Contains(allOutput, dir) {
		t.Fatalf("state audit leaked secret data or inspected filesystem paths: %q", allOutput)
	}
	for _, want := range []string{"System gateway credential metadata: present", "User gateway credential metadata: present"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in audit result %q", want, out.String())
		}
	}
}
