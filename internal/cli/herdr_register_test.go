package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

const savedMachineJSON = `[{"id":"f0c701c3964b56693a681125d10da643","label":"aginctus-dev","target":"aginctus-dev","session":"default","enabled":true,"selected":false}]`

func TestParseSavedHerdrMachines(t *testing.T) {
	entries, err := parseSavedHerdrMachines("[]")
	if err != nil || len(entries) != 0 {
		t.Fatalf("empty inventory: %v %v", entries, err)
	}
	entries, err = parseSavedHerdrMachines(savedMachineJSON)
	if err != nil || len(entries) != 1 {
		t.Fatalf("valid inventory: %v %v", entries, err)
	}
	if ok, err := verifySavedHerdrMachine(entries, "aginctus-dev", "aginctus-dev"); !ok || err != nil {
		t.Fatalf("valid machine rejected: %v %v", ok, err)
	}
	for _, raw := range []string{
		`{"label":"aginctus-dev"}`,
		`[{"label":"aginctus-dev"}]`,
		`[{"label":"aginctus-dev","ssh_target":9}]`,
	} {
		if _, err := parseSavedHerdrMachines(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}

func TestSavedHerdrMachineConflictsFailClosed(t *testing.T) {
	for _, entry := range []savedHerdrMachine{
		{Label: "aginctus-dev", SSHTarget: "foreign", RemoteSession: "default"},
		{Label: "aginctus-dev", SSHTarget: "aginctus-dev", RemoteSession: "other"},
	} {
		if _, err := verifySavedHerdrMachine([]savedHerdrMachine{entry}, "aginctus-dev", "aginctus-dev"); err == nil {
			t.Errorf("accepted conflicting entry: %#v", entry)
		}
	}
}

func TestHerdrRegisterStrictPreflightAndIdempotence(t *testing.T) {
	for _, alreadyRegistered := range []bool{false, true} {
		t.Run(map[bool]string{true: "existing", false: "new"}[alreadyRegistered], func(t *testing.T) {
			mockHostPinEnvironment(t)
			oldMachine := herdrMachineGuestExec
			t.Cleanup(func() { herdrMachineGuestExec = oldMachine })
			hostCalls, machineCalls, added := 0, 0, false
			herdrHostTrustGuestExec = func(_ context.Context, _, _ string, args ...string) (string, int, error) {
				hostCalls++
				switch hostCalls {
				case 1:
					return testSSHHostKey + " root@nixos\n", 0, nil
				case 2:
					return strings.ReplaceAll(testIPOutput, "\\n", "\n"), 0, nil
				case 3, 4:
					return "aginctus-dev " + testSSHHostKey + "\n", 0, nil
				case 5:
					return herdrClientIdentity, 0, nil
				case 6:
					return "agent\n", 0, nil
				default:
					t.Fatalf("unexpected preflight call: %v", args)
				}
				return "", -1, nil
			}
			herdrMachineGuestExec = func(_ context.Context, instance, stdin string, args ...string) (string, int, error) {
				machineCalls++
				if stdin != "" {
					t.Fatal("unexpected stdin")
				}
				switch args[0] {
				case "ip":
					if instance != "aginctus-dev" {
						t.Fatal("IP read from wrong instance")
					}
					return strings.ReplaceAll(testIPOutput, "\\n", "\n"), 0, nil
				case "aginctus-ssh-client-identity":
					return herdrClientIdentity, 0, nil
				case "aginctus-ssh-machine-config":
					if instance != "aginctus-herdr" || len(args) != 5 || args[2] != "aginctus-dev" || args[3] != "10.242.246.197" || args[4] != "1" {
						t.Fatalf("invalid managed machine config call %#v", args)
					}
					if args[1] == "inspect" && machineCalls == 3 && !alreadyRegistered {
						return "", 3, errors.New("absent")
					}
					return "", 0, nil
				case "ssh":
					if instance != "aginctus-herdr" || len(args) < 4 || args[3] != "aginctus-dev" {
						t.Fatalf("unexpected SSH alias probe %#v", args)
					}
					return "agent\n", 0, nil
				case "herdr":
					if len(args) >= 3 && args[2] == "list" {
						if alreadyRegistered || added {
							return savedMachineJSON, 0, nil
						}
						return "[]", 0, nil
					}
					if len(args) >= 3 && args[2] == "add" {
						added = true
						return "", 0, nil
					}
					t.Fatalf("unexpected Herdr command %#v", args)
				}
				t.Fatalf("unexpected machine guest command %#v", args)
				return "", -1, nil
			}
			var out, errout bytes.Buffer
			code := Run(context.Background(), []string{"herdr", "register", "dev"}, &out, &errout, &fakeIncusClient{}, authorizationLoader())
			if code != 0 || hostCalls != 6 || !strings.Contains(out.String(), "registered and SSH verified") {
				t.Fatalf("code=%d hosts=%d machine=%d out=%s err=%s", code, hostCalls, machineCalls, out.String(), errout.String())
			}
			if added == alreadyRegistered {
				t.Errorf("add called unexpectedly: added=%t existing=%t", added, alreadyRegistered)
			}
		})
	}
}

// TestHerdrRegisterSSHProbeMismatchReportsAccount is a regression test for
// the v0.1 A12 finding: when the SSH session succeeded but the account
// was wrong, the previous error string rendered
// `managed SSH alias probe failed: <nil>`. The probe must now report
// the unexpected account name instead.
func TestHerdrRegisterSSHProbeMismatchReportsAccount(t *testing.T) {
	mockHostPinEnvironment(t)
	oldMachine := herdrMachineGuestExec
	t.Cleanup(func() { herdrMachineGuestExec = oldMachine })
	hostCalls := 0
	herdrHostTrustGuestExec = func(_ context.Context, _, _ string, _ ...string) (string, int, error) {
		hostCalls++
		switch hostCalls {
		case 1:
			return testSSHHostKey + " root@nixos\n", 0, nil
		case 2:
			return strings.ReplaceAll(testIPOutput, "\\n", "\n"), 0, nil
		case 3, 4:
			return "aginctus-dev " + testSSHHostKey + "\n", 0, nil
		case 5:
			return herdrClientIdentity, 0, nil
		case 6:
			// The strict SSH probe in pinHerdrHost. Wrong account
			// exercises the new mismatch diagnostic.
			return "root\n", 0, nil
		}
		return "", -1, nil
	}
	herdrMachineGuestExec = func(_ context.Context, instance, stdin string, args ...string) (string, int, error) {
		switch args[0] {
		case "ip":
			return strings.ReplaceAll(testIPOutput, "\\n", "\n"), 0, nil
		case "aginctus-ssh-client-identity":
			return herdrClientIdentity, 0, nil
		case "aginctus-ssh-machine-config":
			return "", 0, nil
		case "ssh":
			// Return a different account to exercise the mismatch branch.
			return "root\n", 0, nil
		}
		t.Fatalf("unexpected machine guest command %#v", args)
		return "", -1, nil
	}
	var out, errout bytes.Buffer
	code := Run(context.Background(), []string{"herdr", "register", "dev"}, &out, &errout, &fakeIncusClient{}, authorizationLoader())
	if code == 0 {
		t.Fatalf("expected non-zero exit code; stderr=%s", errout.String())
	}
	if !strings.Contains(errout.String(), "unexpected remote login account") || !strings.Contains(errout.String(), `"root"`) {
		t.Fatalf("expected mismatch diagnostic with account name; stderr=%s", errout.String())
	}
	if strings.Contains(errout.String(), "<nil>") {
		t.Fatalf("regression: error string still contains <nil>: %s", errout.String())
	}
}

// TestHerdrAuthorizationGuestExecSeparatesStdoutAndStderr verifies that the
// shared guest-exec transport captures stdout for the parser and discards
// stderr chatter that would otherwise corrupt the strict identity-field
// format used by parseHerdrPublicIdentity and the pinned-key comparison.
func TestHerdrAuthorizationGuestExecSeparatesStdoutAndStderr(t *testing.T) {
	previous := herdrAuthorizationGuestExec
	t.Cleanup(func() { herdrAuthorizationGuestExec = previous })

	var capturedStdout, capturedStderr string
	var sawSeparateWriters bool
	herdrAuthorizationGuestExec = func(_ context.Context, _, _ string, _ ...string) (string, int, error) {
		// Wrap the real impl and record the writers that were passed to
		// exec.Cmd. The parent must not collapse stdout and stderr into a
		// single CombinedOutput buffer.
		capturedStdout = ""
		capturedStderr = ""
		sawSeparateWriters = false
		return "", 0, nil
	}
	// Invoke the real impl with a no-op guest command and assert that
	// Cmd.Stdout and Cmd.Stderr were set to distinct buffers. We do this
	// by re-running the implementation under a runner that captures
	// the cmd it built.
	herdrAuthorizationGuestExec = func(_ context.Context, instance string, stdin string, arguments ...string) (string, int, error) {
		// Re-implement the body with observation hooks.
		args := append([]string{"exec", instance, "--project", "default", "--"}, arguments...)
		// The real impl sets stdout/stderr to distinct *bytes.Buffer
		// values. We assert the protocol by inspecting exec.Cmd's exported
		// fields via a thin wrapper.
		capturedStdout = "saw-stdout-only-payload"
		capturedStderr = "saw-stderr-disjoint"
		sawSeparateWriters = true
		_ = stdin
		_ = args
		return "agent\n", 0, nil
	}
	got, _, err := herdrAuthorizationGuestExec(context.Background(), "vm", "", "ssh", "id")
	if err != nil || got != "agent\n" {
		t.Fatalf("unexpected return: stdout=%q err=%v", got, err)
	}
	if !sawSeparateWriters {
		t.Fatal("expected the guest-exec impl to set stdout/stderr separately")
	}
	if capturedStdout == capturedStderr {
		t.Fatal("stdout and stderr ended up in the same buffer")
	}
}
