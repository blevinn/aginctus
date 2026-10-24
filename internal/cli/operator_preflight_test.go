package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func stubOperatorIncus(t *testing.T, fn func(...string) ([]byte, error)) {
	t.Helper()
	previous := inspectOperatorIncus
	inspectOperatorIncus = func(_ context.Context, args ...string) ([]byte, error) {
		return fn(args...)
	}
	t.Cleanup(func() { inspectOperatorIncus = previous })
}

func TestOperatorPreflightProjectReadOnly(t *testing.T) {
	var calls []string
	stubOperatorIncus(t, func(args ...string) ([]byte, error) {
		call := strings.Join(args, " ")
		calls = append(calls, call)
		switch call {
		case "info":
			return []byte("Server: Incus"), nil
		case "list --project sandbox --format json":
			return []byte(`[{"name":"guest"}]`), nil
		case "project list --format json":
			return []byte(`[{"name":"sandbox"}]`), nil
		default:
			t.Fatalf("unexpected command %q", call)
			return nil, nil
		}
	})
	var out, errBuf bytes.Buffer
	code := runOperatorPreflight(context.Background(), []string{"preflight", "--project=sandbox"}, &out, &errBuf)
	if code != 0 || errBuf.Len() != 0 {
		t.Fatalf("status=%d stderr=%q", code, errBuf.String())
	}
	for _, required := range []string{
		"daemon: readable", "project: sandbox (instance read allowed)",
		"Visible Incus projects: 1", "Write permissions: unverified",
		"Host bridge, firewall, pool and admin permissions: unverified",
	} {
		if !strings.Contains(out.String(), required) {
			t.Errorf("missing %q in output %q", required, out.String())
		}
	}
	want := []string{"info", "list --project sandbox --format json", "project list --format json"}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("actual commands %#v, want %#v", calls, want)
	}
}

func TestOperatorPreflightNoProjectOrInvalidProject(t *testing.T) {
	stubOperatorIncus(t, func(args ...string) ([]byte, error) {
		t.Fatalf("should not invoke Incus: %#v", args)
		return nil, nil
	})
	for _, args := range [][]string{
		{"preflight"}, {"preflight", "--project", "../escape"},
		{"preflight", "--project", ""}, {"preflight", "--project=bad name"},
		{"preflight", "--project", "sandbox", "--force"},
		{"unknown", "--project", "sandbox"},
	} {
		var out, errBuf bytes.Buffer
		if code := runOperatorPreflight(context.Background(), args, &out, &errBuf); code != 2 {
			t.Errorf("args %#v: status=%d, want 2", args, code)
		}
	}
}

func TestOperatorPreflightOfflineOrDenied(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fail  string
		count int
	}{
		{"offline", "info", 1},
		{"project denied", "list --project sandbox --format json", 2},
		{"invalid instance response", "malformed", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			stubOperatorIncus(t, func(args ...string) ([]byte, error) {
				calls++
				key := strings.Join(args, " ")
				if key == tc.fail {
					return nil, errors.New("private-incus-access-token-must-never-be-printed")
				}
				if tc.fail == "malformed" && strings.HasPrefix(key, "list ") {
					return []byte("not json"), nil
				}
				return []byte("[]"), nil
			})
			var out, errBuf bytes.Buffer
			code := runOperatorPreflight(context.Background(), []string{"preflight", "--project", "sandbox"}, &out, &errBuf)
			if code != 1 || calls != tc.count {
				t.Fatalf("status=%d calls=%d, wanted status=1 calls=%d", code, calls, tc.count)
			}
			if strings.Contains(errBuf.String(), "private-incus-access-token") {
				t.Fatalf("leaked subprocess error: %s", errBuf.String())
			}
		})
	}
}

func TestOperatorPreflightProjectEnumerationIsOptional(t *testing.T) {
	stubOperatorIncus(t, func(args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "project list --format json" {
			return nil, errors.New("not permitted")
		}
		return []byte("[]"), nil
	})
	var out, errBuf bytes.Buffer
	if code := runOperatorPreflight(context.Background(), []string{"preflight", "--project", "sandbox"}, &out, &errBuf); code != 0 {
		t.Fatalf("status=%d stderr=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "Project enumeration: unavailable") {
		t.Fatalf("missing conservative result: %q", out.String())
	}
}
