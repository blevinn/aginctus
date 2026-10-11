package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
)

const testSSHHostKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAII3xO5DA+NU/hp7fMjqfAVT8P9zXaPn86RUQxRw2h1Oc"
const testIPOutput = "2: eth0    inet 10.242.246.197/24 brd 10.242.246.255 scope global dynamic eth0\\n"

func TestParseWorkloadHostKey(t *testing.T) {
	key, err := parseWorkloadHostKey(testSSHHostKey + " root@nixos\n")
	if err != nil || key != testSSHHostKey {
		t.Fatalf("host key=%q err=%v", key, err)
	}
	for _, input := range []string{"", "ssh-rsa AAAA host", "ssh-ed25519 malformed comment", testSSHHostKey + " extra words comment", "ssh-ed25519 AAAA host"} {
		if _, err := parseWorkloadHostKey(input); err == nil {
			t.Fatalf("accepted invalid host key %q", input)
		}
	}
}

func TestParseManagementAddress(t *testing.T) {
	ip, err := parseManagementAddress(strings.ReplaceAll(testIPOutput, "\\n", "\n"))
	if err != nil || ip != "10.242.246.197" {
		t.Fatalf("IP=%q err=%v", ip, err)
	}
	for _, raw := range []string{"", "inet 127.0.0.1/8", "inet 169.254.1.1/16", "inet 10.0.0.1/24 inet 10.0.0.2/24"} {
		if _, err := parseManagementAddress(raw); err == nil {
			t.Fatalf("accepted invalid address %q", raw)
		}
	}
}

func mockHostPinEnvironment(t *testing.T) {
	t.Helper()
	oldInspect, oldExec := inspectIncusInstances, herdrHostTrustGuestExec
	t.Cleanup(func() { inspectIncusInstances, herdrHostTrustGuestExec = oldInspect, oldExec })
	inspectIncusInstances = func(context.Context) ([]byte, error) { return []byte(ownedAuthorizationInstances), nil }
}

func TestHerdrPinHostReconcilesAndProbesStrictly(t *testing.T) {
	mockHostPinEnvironment(t)
	var calls []string
	herdrHostTrustGuestExec = func(_ context.Context, instance, stdin string, args ...string) (string, int, error) {
		calls = append(calls, instance+":"+strings.Join(args, " "))
		switch len(calls) {
		case 1:
			return testSSHHostKey + " root@nixos\n", 0, nil
		case 2:
			return strings.ReplaceAll(testIPOutput, "\\n", "\n"), 0, nil
		case 3:
			return "", 3, errors.New("not pinned")
		case 4:
			if stdin != testSSHHostKey+"\n" {
				t.Fatalf("trust received unexpected public key: %q", stdin)
			}
			return "aginctus-dev " + testSSHHostKey + "\n", 0, nil
		case 5:
			return "aginctus-dev " + testSSHHostKey + "\n", 0, nil
		case 6:
			return herdrClientIdentity, 0, nil
		case 7:
			probe := strings.Join(args, " ")
			for _, required := range []string{"StrictHostKeyChecking=yes", "IdentitiesOnly=yes", "IdentityAgent=none", "HostKeyAlias=aginctus-dev", "HostKeyAlgorithms=ssh-ed25519", "agent@10.242.246.197", "/identities/1/id_ed25519"} {
				if !strings.Contains(probe, required) {
					t.Errorf("missing %s in %s", required, probe)
				}
			}
			return "agent\n", 0, nil
		}
		t.Fatal("unexpected extra guest invocation")
		return "", -1, nil
	}
	var out, stderr bytes.Buffer
	got := Run(context.Background(), []string{"herdr", "pin-host", "dev"}, &out, &stderr, &fakeIncusClient{}, authorizationLoader())
	if got != 0 || len(calls) != 7 || !strings.Contains(out.String(), "strict SSH probe passed") {
		t.Fatalf("exit=%d calls=%v stdout=%q stderr=%q", got, calls, out.String(), stderr.String())
	}
}

func TestHerdrPinHostRejectsMismatchedExistingTrust(t *testing.T) {
	mockHostPinEnvironment(t)
	calls := 0
	herdrHostTrustGuestExec = func(_ context.Context, instance, stdin string, args ...string) (string, int, error) {
		calls++
		if stdin != "" {
			t.Fatal("attempt to overwrite existing pin")
		}
		switch calls {
		case 1:
			return testSSHHostKey + " root@nixos\n", 0, nil
		case 2:
			return strings.ReplaceAll(testIPOutput, "\\n", "\n"), 0, nil
		case 3:
			return "aginctus-dev ssh-ed25519 AAAA\n", 0, nil
		}
		t.Fatal("unexpected guest command")
		return "", -1, nil
	}
	loader := authorizationLoader()
	cfg, err := loader.Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	err = pinHerdrHost(context.Background(), cfg, "dev")
	if err == nil || !strings.Contains(err.Error(), "pinned host key differs") || calls != 3 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestHerdrPinHostFailsWhenStrictSSHBlocked(t *testing.T) {
	mockHostPinEnvironment(t)
	calls := 0
	herdrHostTrustGuestExec = func(_ context.Context, instance, stdin string, args ...string) (string, int, error) {
		calls++
		switch calls {
		case 1:
			return testSSHHostKey + " root@nixos\n", 0, nil
		case 2:
			return strings.ReplaceAll(testIPOutput, "\\n", "\n"), 0, nil
		case 3, 4:
			return "aginctus-dev " + testSSHHostKey + "\n", 0, nil
		case 5:
			return herdrClientIdentity, 0, nil
		case 6:
			return "", 255, errors.New("ssh unreachable")
		}
		t.Fatal("unexpected guest command")
		return "", -1, nil
	}
	loader := authorizationLoader()
	cfg, err := loader.Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	err = pinHerdrHost(context.Background(), cfg, "dev")
	if err == nil || !strings.Contains(err.Error(), "strict SSH probe failed") {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
