package images

import (
	"context"
	"errors"
	"strings"
	"testing"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

func TestScopedAliasFingerprintFailsClosed(t *testing.T) {
	scope, err := incusscope.NewProjectScope("aginctus")
	if err != nil { t.Fatal(err) }
	for _, tc := range []struct {
		name, payload string
		fail bool
		max int
	}{
		{"missing", "null", false, 1024},
		{"malformed", "{", false, 1024},
		{"empty fingerprint", `[{"fingerprint":"","aliases":[{"name":"target"}]}]`, false, 1024},
		{"duplicate alias", `[{"fingerprint":"one","aliases":[{"name":"target"}]},{"fingerprint":"two","aliases":[{"name":"target"}]}]`, false, 1024},
		{"oversized", "[]", false, 1},
		{"transport", "[]", true, 1024},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ScopedAliasFingerprint(context.Background(), scope, "target",
				func(context.Context, incusscope.ProjectScope) ([]byte, error) {
					if tc.fail { return nil, errors.New("private transport detail") }
					return []byte(tc.payload), nil
				}, tc.max)
			if err == nil { t.Fatal("unsafe inventory accepted") }
			if strings.Contains(err.Error(), "private transport detail") { t.Fatal("transport detail leaked") }
		})
	}
	if _, _, err := ScopedAliasFingerprint(context.Background(), scope, "target", nil, 100); err == nil {
		t.Fatal("nil inspector accepted")
	}
}

func TestScopedAliasFingerprintInventory(t *testing.T) {
	scope, err := incusscope.NewProjectScope("aginctus")
	if err != nil { t.Fatal(err) }
	var called bool
	inspect := func(_ context.Context, got incusscope.ProjectScope) ([]byte, error) {
		called = true
		if got.Name != scope.Name { t.Fatalf("unexpected project %q", got.Name) }
		return []byte(`[{"fingerprint":"abc","aliases":[{"name":"target"}]}]`), nil
	}
	fp, exists, err := ScopedAliasFingerprint(context.Background(), scope, "target", inspect, 1024)
	if err != nil || !exists || fp != "abc" || !called { t.Fatalf("fp=%q exists=%v called=%v err=%v", fp, exists, called, err) }
	fp, exists, err = ScopedAliasFingerprint(context.Background(), scope, "other", inspect, 1024)
	if err != nil || exists || fp != "" { t.Fatalf("unexpected absent alias: %q %v %v", fp, exists, err) }
}

func TestVerifyScopedImageProject(t *testing.T) {
	scope, err := incusscope.NewProjectScope("aginctus")
	if err != nil { t.Fatal(err) }
	if err := VerifyScopedImageProject(context.Background(), scope, nil); err == nil { t.Fatal("nil verifier accepted") }
	for _, tc := range []struct { status string; err error; wantError bool }{
		{string(incusscope.ProjectCompatible), nil, false},
		{"unverified", nil, true},
		{string(incusscope.ProjectCompatible), errors.New("sensitive"), true},
	} {
		err := VerifyScopedImageProject(context.Background(), scope, func(_ context.Context, got incusscope.ProjectScope) (string, error) {
			if got.Name != scope.Name { t.Fatalf("unexpected scope %q", got.Name) }
			return tc.status, tc.err
		})
		if (err != nil) != tc.wantError { t.Fatalf("status %q: %v", tc.status, err) }
		if err != nil && strings.Contains(err.Error(), "sensitive") { t.Fatal("private failure leaked") }
	}
}
