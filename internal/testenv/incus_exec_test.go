package testenv

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeGuestIncusClient struct {
	*fakeIncusVMClient
	argv     []string
	response VMExecResult
	err      error
}

func (c *fakeGuestIncusClient) ExecGuest(_ context.Context, _ string, argv []string) (VMExecResult, error) {
	c.argv = append([]string(nil), argv...)
	return c.response, c.err
}

func TestIncusGuestExecUsesOwnedAgentAndLiteralArgv(t *testing.T) {
	ctx := context.Background()
	fake := &fakeGuestIncusClient{fakeIncusVMClient: newFakeIncusClient(t),
		response: VMExecResult{ExitCode: 17, Stdout: "ok", Stderr: "not fatal"}}
	backend, err := newTestIncusBackend(fake, "testenv")
	if err != nil {
		t.Fatal(err)
	}
	backend.now = time.Now
	ref, err := backend.Create(ctx, admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	if !backend.Capabilities().Exec || backend.Capabilities().Snapshot || backend.Capabilities().Reset {
		t.Fatalf("incorrect backend capabilities: %#v", backend.Capabilities())
	}
	args := []string{"/run/current-system/sw/bin/echo", "x;touch /tmp/foo", "$(id)"}
	out, err := backend.Exec(ctx, ref, args, 2*time.Second)
	if err != nil || out.ExitCode != 17 || out.Stdout != "ok" || out.Stderr != "not fatal" ||
		strings.Join(fake.argv, "\x00") != strings.Join(args, "\x00") {
		t.Fatalf("guest exec altered argv/exit: %#v %#v %v", out, fake.argv, err)
	}
}

func TestIncusGuestExecRejectsUnsafeArgvAndLimits(t *testing.T) {
	ctx := context.Background()
	fake := &fakeGuestIncusClient{fakeIncusVMClient: newFakeIncusClient(t)}
	backend, _ := newTestIncusBackend(fake, "testenv")
	backend.now = time.Now
	ref, err := backend.Create(ctx, admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		argv    []string
		timeout time.Duration
	}{
		{nil, time.Second},
		{[]string{"sh", "-c", "id"}, time.Second},
		{[]string{"/bin/../sh"}, time.Second},
		{[]string{"/bin/echo", "abc\x00def"}, time.Second},
		{[]string{"/bin/echo", strings.Repeat("x", 8193)}, time.Second},
		{append([]string{"/bin/echo"}, make([]string, 32)...), time.Second},
		{[]string{"/bin/true"}, 0},
		{[]string{"/bin/true"}, 31 * time.Second},
	} {
		if _, err := backend.Exec(ctx, ref, tc.argv, tc.timeout); err == nil {
			t.Fatalf("accepted unsafe guest exec args %#v timeout %s", tc.argv, tc.timeout)
		}
		if len(fake.argv) != 0 {
			t.Fatalf("invalid exec reached guest: %#v", fake.argv)
		}
	}
}

func TestIncusGuestExecAcceptsExactArgvBoundaries(t *testing.T) {
	if err := validateIncusExec(append([]string{"/bin/echo"}, make([]string, 31)...), time.Second); err != nil {
		t.Fatal(err)
	}
	if err := validateIncusExec([]string{"/bin/echo", strings.Repeat("x", maxIncusExecArgBytes-len("/bin/echo"))}, maxIncusExecDuration); err != nil {
		t.Fatal(err)
	}
}

func TestIncusGuestExecRequiresActiveOwnedRun(t *testing.T) {
	ctx := context.Background()
	fake := &fakeGuestIncusClient{fakeIncusVMClient: newFakeIncusClient(t)}
	backend, _ := newTestIncusBackend(fake, "testenv")
	backend.now = time.Now
	ref, err := backend.Create(ctx, admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	tampered := ref
	tampered.ImageFingerprint = strings.Repeat("f", 64)
	if _, err := backend.Exec(ctx, tampered, []string{"/bin/true"}, time.Second); !errors.Is(err, ErrOwnershipMismatch) {
		t.Fatalf("unowned guest exec admitted: %v", err)
	}
	vm := fake.vms[ref.Instance]
	vm.Status = "Stopped"
	fake.vms[ref.Instance] = vm
	if _, err := backend.Exec(ctx, ref, []string{"/bin/true"}, time.Second); err == nil {
		t.Fatal("stopped guest exec admitted")
	}
	vm.Status = "Running"
	fake.vms[ref.Instance] = vm
	backend.now = func() time.Time { return time.Now().Add(time.Hour) }
	if _, err := backend.Exec(ctx, ref, []string{"/bin/true"}, time.Second); err == nil {
		t.Fatal("expired guest exec admitted")
	}
	if len(fake.argv) != 0 {
		t.Fatalf("unsafe guest received argv: %v", fake.argv)
	}
}

func TestIncusGuestExecReportsOverflowAndTransportFailure(t *testing.T) {
	ctx := context.Background()
	fake := &fakeGuestIncusClient{fakeIncusVMClient: newFakeIncusClient(t)}
	backend, _ := newTestIncusBackend(fake, "testenv")
	backend.now = time.Now
	ref, err := backend.Create(ctx, admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	fake.response.Stdout = strings.Repeat("s", MaxGuestOutput+1)
	if _, err := backend.Exec(ctx, ref, []string{"/bin/true"}, time.Second); !errors.Is(err, ErrGuestOutputLimit) {
		t.Fatalf("oversized output not rejected: %v", err)
	}
	fake.response = VMExecResult{}
	fake.err = errors.New("guest provider said sensitive credential")
	if _, err := backend.Exec(ctx, ref, []string{"/bin/true"}, time.Second); err == nil ||
		strings.Contains(err.Error(), "sensitive credential") {
		t.Fatalf("transport failure leaked untrusted text: %v", err)
	}
}
