package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const operatorServerQuery = "query --raw --request GET /1.0?project=sandbox"
const operatorInstancesQuery = "query --raw --request GET /1.0/instances?project=sandbox&recursion=1"

func operatorAPIResponse(metadata string) []byte {
	return []byte(`{"type":"sync","status_code":200,"metadata":` + metadata + `}`)
}

func operatorTrustedResponse() []byte {
	return operatorAPIResponse(`{"auth":"trusted","auth_user_method":"unix"}`)
}

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
		case operatorServerQuery:
			return operatorTrustedResponse(), nil
		case operatorInstancesQuery:
			return operatorAPIResponse(`[{"name":"guest"}]`), nil
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
	want := []string{operatorServerQuery, operatorInstancesQuery, "project list --format json"}
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
		{"preflight", "--project", "default"},
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
		{"offline", operatorServerQuery, 1},
		{"project denied", operatorInstancesQuery, 2},
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
				if tc.fail == "malformed" && key == operatorInstancesQuery {
					return []byte("not json"), nil
				}
				return operatorTrustedResponse(), nil
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
		if strings.Join(args, " ") == operatorServerQuery {
			return operatorTrustedResponse(), nil
		}
		return operatorAPIResponse("[]"), nil
	})
	var out, errBuf bytes.Buffer
	if code := runOperatorPreflight(context.Background(), []string{"preflight", "--project", "sandbox"}, &out, &errBuf); code != 0 {
		t.Fatalf("status=%d stderr=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "Project enumeration: unavailable") {
		t.Fatalf("missing conservative result: %q", out.String())
	}
}

func TestOperatorPreflightIdentityEvidence(t *testing.T) {
	fingerprint := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, server, cert, want string
		code                     int
	}{
		{"untrusted", `{"auth":"untrusted"}`, "", "unauthenticated", 1},
		{"unknown auth", `{}`, "", "unproven", 1},
		{"unix", `{"auth":"trusted","auth_user_method":"unix"}`, "", "administrative capabilities unproven", 0},
		{"restricted", `{"auth":"trusted","auth_user_method":"tls","auth_user_name":"` + fingerprint + `"}`, `{"fingerprint":"` + fingerprint + `","restricted":true,"projects":["sandbox"]}`, "restricted TLS credential; selected project declared permitted", 0},
		{"broad", `{"auth":"trusted","auth_user_method":"tls","auth_user_name":"` + fingerprint + `"}`, `{"fingerprint":"` + fingerprint + `","restricted":false}`, "broad unrestricted TLS credential; effective administrative capabilities unproven", 0},
		{"foreign restriction", `{"auth":"trusted","auth_user_method":"tls","auth_user_name":"` + fingerprint + `"}`, `{"fingerprint":"` + fingerprint + `","restricted":true,"projects":["other"]}`, "selected project not declared permitted", 1},
		{"missing restriction", `{"auth":"trusted","auth_user_method":"tls","auth_user_name":"` + fingerprint + `"}`, `{"fingerprint":"` + fingerprint + `"}`, "restriction unproven", 0},
		{"wrong certificate", `{"auth":"trusted","auth_user_method":"tls","auth_user_name":"` + fingerprint + `"}`, `{"fingerprint":"wrong","restricted":false}`, "restriction unproven", 0},
		{"self certificate denied", `{"auth":"trusted","auth_user_method":"tls","auth_user_name":"` + fingerprint + `"}`, "denied", "restriction unavailable", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubOperatorIncus(t, func(args ...string) ([]byte, error) {
				switch strings.Join(args, " ") {
				case operatorServerQuery:
					return operatorAPIResponse(tc.server), nil
				case "query --raw --request GET /1.0/certificates/" + fingerprint:
					if tc.cert == "denied" {
						return []byte(`{"type":"error","error_code":403,"error":"synthetic-private-token"}`), nil
					}
					return operatorAPIResponse(tc.cert), nil
				case operatorInstancesQuery:
					return operatorAPIResponse("[]"), nil
				case "project list --format json":
					return []byte(`[{"name":"sandbox"},{"name":"other"}]`), nil
				default:
					t.Fatalf("unexpected command: %#v", args)
					return nil, nil
				}
			})
			var out, errBuf bytes.Buffer
			if code := runOperatorPreflight(context.Background(), []string{"preflight", "--project", "sandbox"}, &out, &errBuf); code != tc.code {
				t.Fatalf("code=%d want=%d: %s %s", code, tc.code, &out, &errBuf)
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("missing %q: %s", tc.want, &out)
			}
			if strings.Contains(out.String()+errBuf.String(), "synthetic-private-token") || strings.Contains(out.String(), fingerprint) {
				t.Fatal("identity material or raw error leaked")
			}
		})
	}
}

func TestOperatorPreflightRejectsNullInstanceInventory(t *testing.T) {
	stubOperatorIncus(t, func(args ...string) ([]byte, error) {
		if strings.Join(args, " ") == operatorServerQuery {
			return operatorTrustedResponse(), nil
		}
		return operatorAPIResponse("null"), nil
	})
	var out, errBuf bytes.Buffer
	if code := runOperatorPreflight(context.Background(), []string{"preflight", "--project", "sandbox"}, &out, &errBuf); code != 1 {
		t.Fatalf("null inventory accepted: %d", code)
	}
}

func TestOperatorProbeFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		err  error
		want operatorReadStatus
	}{
		{"denied", []byte(`{"type":"error","error_code":403,"error":"private-token"}`), nil, operatorReadDenied},
		{"no identity", []byte(`{"type":"error","error_code":401}`), nil, operatorReadNoIdentity},
		{"server error", []byte(`{"type":"error","error_code":500}`), nil, operatorReadUnavailable},
		{"unsupported", []byte(`{"type":"error","error_code":404}`), nil, operatorReadUnavailable},
		{"missing", []byte(`{}`), nil, operatorReadInvalid},
		{"null", []byte(`null`), nil, operatorReadInvalid},
		{"malformed", []byte(`not json`), nil, operatorReadInvalid},
		{"oversize", make([]byte, (1<<20)+1), nil, operatorReadInvalid},
		{"contradictory", []byte(`{"type":"sync","status_code":200,"error_code":403,"metadata":{}}`), nil, operatorReadInvalid},
		{"transport", nil, errors.New("private-token"), operatorReadUnavailable},
		{"no CLI", nil, exec.ErrNotFound, operatorReadNoCLI},
		{"local permission", nil, os.ErrPermission, operatorReadDenied},
		{"canceled", nil, context.Canceled, operatorReadCanceled},
		{"timeout", nil, context.DeadlineExceeded, operatorReadTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubOperatorIncus(t, func(...string) ([]byte, error) { return tc.data, tc.err })
			_, got := readOperatorAPI(context.Background(), "/1.0")
			if got != tc.want {
				t.Fatalf("status=%s want=%s", got, tc.want)
			}
		})
	}
	// Ensure a canceled parent performs no subprocess, even before its first read.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stubOperatorIncus(t, func(...string) ([]byte, error) { t.Fatal("probe after cancellation"); return nil, nil })
	if _, got := readOperatorAPI(ctx, "/1.0"); got != operatorReadCanceled {
		t.Fatal(got)
	}
}

func TestOperatorPreflightCancellationDuringOptionalRead(t *testing.T) {
	previous := inspectOperatorIncus
	t.Cleanup(func() { inspectOperatorIncus = previous })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inspectOperatorIncus = func(probeCtx context.Context, args ...string) ([]byte, error) {
		deadline, ok := probeCtx.Deadline()
		if !ok || time.Until(deadline) > 15*time.Second {
			t.Fatal("unbounded probe context")
		}
		switch strings.Join(args, " ") {
		case operatorServerQuery:
			return operatorTrustedResponse(), nil
		case operatorInstancesQuery:
			return operatorAPIResponse("[]"), nil
		case "project list --format json":
			cancel()
			return []byte("[]"), nil
		default:
			t.Fatalf("unexpected command: %#v", args)
			return nil, nil
		}
	}
	var out, errBuf bytes.Buffer
	if code := runOperatorPreflight(ctx, []string{"preflight", "--project", "sandbox"}, &out, &errBuf); code != 1 {
		t.Fatalf("canceled optional probe returned %d", code)
	}
	if !strings.Contains(errBuf.String(), "canceled") {
		t.Fatalf("missing cancellation diagnostic: %s", &errBuf)
	}
}
