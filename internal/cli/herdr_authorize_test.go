package cli

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
)

const herdrClientIdentity = "generation\t1\nfingerprint\tSHA256:test\npublic_key\tssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIERJKHHDrpkZbZNyrEgH8F4Z63MnI1tMUI0RB0Gb0cSR aginctus:1\n"
const herdrClientKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIERJKHHDrpkZbZNyrEgH8F4Z63MnI1tMUI0RB0Gb0cSR aginctus:1"
const ownedAuthorizationInstances = `[
  {"name":"aginctus-herdr","status":"Running","project":"default","config":{"user.aginctus.managed":"true","user.aginctus.resource":"infrastructure","user.aginctus.role":"herdr-client"}},
  {"name":"aginctus-dev","status":"Running","project":"default","config":{"user.aginctus.managed":"true","user.aginctus.resource":"workload","user.aginctus.role":"agent-workload","user.aginctus.workload":"dev"}}
]`

func authorizationLoader() *config.Loader {
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	return loader
}

func TestParseHerdrPublicIdentity(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		valid bool
	}{
		{"valid", herdrClientIdentity, true},
		{"missing", "generation\t1\n", false},
		{"duplicate", herdrClientIdentity + "public_key\t" + herdrClientKey + "\n", false},
		{"generation mismatch", strings.ReplaceAll(herdrClientIdentity, "generation\t1", "generation\t2"), false},
		{"non-managed", strings.ReplaceAll(herdrClientIdentity, "aginctus:1", "human"), false},
		{"extra field", herdrClientIdentity + "unexpected\tvalue\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key, err := parseHerdrPublicIdentity(tc.input)
			if (err == nil) != tc.valid {
				t.Fatalf("key=%q err=%v, valid=%v", key, err, tc.valid)
			}
			if tc.valid && key != herdrClientKey {
				t.Fatalf("key = %q", key)
			}
		})
	}
}

func TestHerdrAuthorizeAbsentKeyReconcilesAndVerifies(t *testing.T) {
	oldInspect, oldExec := inspectIncusInstances, herdrAuthorizationGuestExec
	t.Cleanup(func() { inspectIncusInstances, herdrAuthorizationGuestExec = oldInspect, oldExec })
	inspectIncusInstances = func(context.Context) ([]byte, error) { return []byte(ownedAuthorizationInstances), nil }
	var calls []string
	herdrAuthorizationGuestExec = func(_ context.Context, instance, stdin string, args ...string) (string, int, error) {
		call := instance + ":" + strings.Join(args, " ")
		calls = append(calls, call)
		switch len(calls) {
		case 1:
			if instance != "aginctus-herdr" || call != "aginctus-herdr:aginctus-ssh-client-identity inspect" {
				t.Fatalf("identity call: %s", call)
			}
			return herdrClientIdentity, 0, nil
		case 2:
			return "", 3, errors.New("not provisioned")
		case 3:
			if stdin != herdrClientKey+"\n" || call != "aginctus-dev:aginctus-ssh-authorize reconcile agent" {
				t.Fatalf("wrong reconcile input or command: %s", call)
			}
			return "", 0, nil
		case 4:
			return herdrClientKey + "\n", 0, nil
		}
		t.Fatalf("unexpected extra call %s", call)
		return "", 1, nil
	}
	var out, stderr bytes.Buffer
	got := Run(context.Background(), []string{"herdr", "authorize", "dev"}, &out, &stderr, &fakeIncusClient{}, authorizationLoader())
	if got != 0 || len(calls) != 4 || !strings.Contains(out.String(), "verified") {
		t.Fatalf("exit=%d calls=%v stdout=%q stderr=%q", got, calls, out.String(), stderr.String())
	}
}

func TestHerdrAuthorizeAlreadyInstalledDoesNotRewrite(t *testing.T) {
	oldInspect, oldExec := inspectIncusInstances, herdrAuthorizationGuestExec
	t.Cleanup(func() { inspectIncusInstances, herdrAuthorizationGuestExec = oldInspect, oldExec })
	inspectIncusInstances = func(context.Context) ([]byte, error) { return []byte(ownedAuthorizationInstances), nil }
	var calls [][]string
	herdrAuthorizationGuestExec = func(_ context.Context, _ string, stdin string, args ...string) (string, int, error) {
		if stdin != "" {
			t.Fatal("unexpected authorization mutation")
		}
		calls = append(calls, args)
		if len(calls) == 1 {
			return herdrClientIdentity, 0, nil
		}
		return herdrClientKey + "\n", 0, nil
	}
	var out, stderr bytes.Buffer
	got := Run(context.Background(), []string{"herdr", "authorize", "dev"}, &out, &stderr, &fakeIncusClient{}, authorizationLoader())
	if got != 0 || len(calls) != 2 || !reflect.DeepEqual(calls[1], []string{"aginctus-ssh-authorize", "inspect", "agent"}) {
		t.Fatalf("exit=%d calls=%v stderr=%q", got, calls, stderr.String())
	}
}

func TestHerdrAuthorizeRejectsUnexpectedExistingKey(t *testing.T) {
	oldInspect, oldExec := inspectIncusInstances, herdrAuthorizationGuestExec
	t.Cleanup(func() { inspectIncusInstances, herdrAuthorizationGuestExec = oldInspect, oldExec })
	inspectIncusInstances = func(context.Context) ([]byte, error) { return []byte(ownedAuthorizationInstances), nil }
	calls := 0
	herdrAuthorizationGuestExec = func(_ context.Context, _ string, stdin string, args ...string) (string, int, error) {
		calls++
		if stdin != "" {
			t.Fatal("unexpected overwrite")
		}
		if calls == 1 {
			return herdrClientIdentity, 0, nil
		}
		return "ssh-ed25519 other aginctus:1\n", 0, nil
	}
	var out, stderr bytes.Buffer
	got := Run(context.Background(), []string{"herdr", "authorize", "dev"}, &out, &stderr, &fakeIncusClient{}, authorizationLoader())
	if got != 1 || calls != 2 || !strings.Contains(stderr.String(), "refusing automatic credential replacement") {
		t.Fatalf("exit=%d calls=%d stderr=%q", got, calls, stderr.String())
	}
}

func TestHerdrAuthorizeRejectsForeignWorkloadBeforeGuestExec(t *testing.T) {
	oldInspect, oldExec := inspectIncusInstances, herdrAuthorizationGuestExec
	t.Cleanup(func() { inspectIncusInstances, herdrAuthorizationGuestExec = oldInspect, oldExec })
	inspectIncusInstances = func(context.Context) ([]byte, error) {
		return []byte(strings.ReplaceAll(ownedAuthorizationInstances, `"user.aginctus.workload":"dev"`, `"user.aginctus.workload":"other"`)), nil
	}
	herdrAuthorizationGuestExec = func(context.Context, string, string, ...string) (string, int, error) {
		t.Fatal("guest helper invoked for unowned workload")
		return "", 1, nil
	}
	var out, stderr bytes.Buffer
	got := Run(context.Background(), []string{"herdr", "authorize", "dev"}, &out, &stderr, &fakeIncusClient{}, authorizationLoader())
	if got != 1 || !strings.Contains(stderr.String(), "ownership markers") {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
}
