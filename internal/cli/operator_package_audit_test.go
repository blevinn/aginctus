package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixturePackageA = "/nix/store/0123456789abcdfghijklmnpqrsvwxyz-aginctus-0.2"
const fixturePackageB = "/nix/store/123456789abcdfghijklmnpqrsvwxyz0-aginctus-0.3"

func writeOperatorPackageFixture(t *testing.T, dir, authority, pkg string) string {
	t.Helper()
	path := filepath.Join(dir, authority+"-package.json")
	value, err := json.Marshal(operatorPackageManifest{
		Schema: 1, Authority: authority, Package: pkg,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, value, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOperatorPackageManifestState(t *testing.T) {
	dir := t.TempDir()
	system := writeOperatorPackageFixture(t, dir, "system", fixturePackageA)
	if state, value := inspectOperatorPackageManifest(system, "system"); state != operatorPackageDeclared || value != fixturePackageA {
		t.Fatalf("valid declaration: %s %q", state, value)
	}
	if state, _ := inspectOperatorPackageManifest(system, "user"); state != operatorPackageUnverified {
		t.Fatalf("wrong role: %s", state)
	}
	if state, _ := inspectOperatorPackageManifest(filepath.Join(dir, "missing"), "system"); state != operatorPackageAbsent {
		t.Fatalf("missing declaration: %s", state)
	}
	bad := filepath.Join(dir, "bad")
	for _, raw := range []string{
		"{not-json}", "null", "{}", 
		`{"schema":2,"authority":"system","package":"` + fixturePackageA + `"}`,
		`{"schema":1,"authority":"system","package":"/var/lib/aginctus/state/secrets.json"}`,
		`{"schema":1,"authority":"system","package":"/nix/store/../secrets"}`,
		strings.Repeat("x", maxOperatorPackageManifestSize+1),
	} {
		if err := os.WriteFile(bad, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if state, value := inspectOperatorPackageManifest(bad, "system"); state != operatorPackageUnverified || value != "" {
			t.Fatalf("invalid declaration: %s %q", state, value)
		}
	}
}

func TestOperatorPackageAuditPair(t *testing.T) {
	dir := t.TempDir()
	system := filepath.Join(dir, "system.json")
	user := filepath.Join(dir, "user.json")
	set := func(path, role, pkg string) {
		t.Helper()
		raw, err := json.Marshal(operatorPackageManifest{Schema: 1, Authority: role, Package: pkg})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	check := func(owner string, wantCode int, want string) {
		t.Helper()
		var out, errOut bytes.Buffer
		code := renderOperatorPackageAudit(owner, system, user, &out, &errOut)
		if code != wantCode || !strings.Contains(out.String()+errOut.String(), want) {
			t.Fatalf("owner=%s code=%d stdout=%q stderr=%q", owner, code, out.String(), errOut.String())
		}
		all := out.String()+errOut.String()
		if strings.Contains(all, fixturePackageA) || strings.Contains(all, fixturePackageB) ||
			strings.Contains(all, dir) || strings.Contains(all, "secrets.json") {
			t.Fatalf("audit printed raw manifest contents or paths: %q", all)
		}
	}
	check("user", 1, "missing or unverified")
	set(user, "user", fixturePackageA)
	check("user", 0, "consistent")
	check("system", 1, "missing or unverified")
	set(system, "system", fixturePackageA)
	check("system", 0, "consistent")
	check("user", 0, "consistent")
	set(user, "user", fixturePackageB)
	check("user", 1, "declarations differ")
	check("system", 1, "declarations differ")
	set(user, "system", fixturePackageA)
	check("user", 1, "missing or unverified")
	if code := renderOperatorPackageAudit("unknown", system, user, &bytes.Buffer{}, &bytes.Buffer{}); code != 2 {
		t.Fatalf("accepted unknown owner: %d", code)
	}
}

func TestOperatorPackageAuditArguments(t *testing.T) {
	for _, tc := range []struct {
		args []string
		ok bool
	}{
		{[]string{"--owner=user"}, true},
		{[]string{"--owner", "system"}, true},
		{[]string{"--owner", "user"}, true},
		{[]string{"--owner=system"}, true},
		{nil, false},
		{[]string{"--owner=root"}, false},
		{[]string{"--owner=user", "--fix"}, false},
		{[]string{"--owner", "user", "--force"}, false},
	} {
		owner, ok := parseOperatorPackageAuditArgs(tc.args)
		if ok != tc.ok || (ok && owner == "") {
			t.Errorf("args=%q accepted=%v owner=%q", tc.args, ok, owner)
		}
	}
}

func TestOperatorPackageAuditRejectsConfigurationOptionsBeforeLoading(t *testing.T) {
	// This validation is reached before Run loads either user or host config.
	for _, args := range [][]string{
		{"--configuration-file=/nonexistent", "operator", "package-audit", "--owner=user"},
		{"--config=gateway.id=test", "operator", "package-audit", "--owner=user"},
	} {
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), args, &stdout, &stderr, nil, nil); code != 2 ||
			stdout.Len() != 0 || !strings.Contains(stderr.String(), "configuration overrides are not supported") {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}
}
