package operatorops

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const packageAuditFixtureA = "/nix/store/0123456789abcdfghijklmnpqrsvwxyz-aginctus-0.2"

func writePackageManifestFixture(t *testing.T, dir, authority, pkg string) string {
	t.Helper()
	path := filepath.Join(dir, authority+"-package.json")
	value, err := json.Marshal(PackageManifest{Schema: 1, Authority: authority, Package: pkg})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, value, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Manifest parsing and argument shape are operation-level policy.
func TestOperatorPackageManifestState(t *testing.T) {
	dir := t.TempDir()
	system := writePackageManifestFixture(t, dir, "system", packageAuditFixtureA)
	if state, value := InspectPackageManifest(system, "system"); state != PackageDeclared || value != packageAuditFixtureA {
		t.Fatalf("valid declaration: %s %q", state, value)
	}
	if state, _ := InspectPackageManifest(system, "user"); state != PackageUnverified {
		t.Fatalf("wrong role: %s", state)
	}
	if state, _ := InspectPackageManifest(filepath.Join(dir, "missing"), "system"); state != PackageAbsent {
		t.Fatalf("missing declaration: %s", state)
	}
	bad := filepath.Join(dir, "bad")
	for _, raw := range []string{
		"{not-json}", "null", "{}",
		`{"schema":2,"authority":"system","package":"` + packageAuditFixtureA + `"}`,
		`{"schema":1,"authority":"system","package":"/var/lib/aginctus/state/secrets.json"}`,
		`{"schema":1,"authority":"system","package":"/nix/store/../secrets"}`,
		strings.Repeat("x", MaxPackageManifestSize+1),
	} {
		if err := os.WriteFile(bad, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if state, value := InspectPackageManifest(bad, "system"); state != PackageUnverified || value != "" {
			t.Fatalf("invalid declaration: %s %q", state, value)
		}
	}
}

func TestOperatorPackageAuditArguments(t *testing.T) {
	for _, tc := range []struct {
		args []string
		ok   bool
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
		owner, ok := ParsePackageAuditArgs(tc.args)
		if ok != tc.ok || (ok && owner == "") {
			t.Errorf("args=%q accepted=%v owner=%q", tc.args, ok, owner)
		}
	}
}
