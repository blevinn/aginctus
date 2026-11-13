package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestDoctorDoesNotFixPreexistingUnsafeHTTPSListener(t *testing.T) {
	previous := setIncusHTTPSAddress
	t.Cleanup(func() { setIncusHTTPSAddress = previous })
	calls := 0
	setIncusHTTPSAddress = func(context.Context, string) error {
		calls++
		return nil
	}
	for _, addr := range []string{"0.0.0.0:8443", "[::]:8443", ":8443", "192.168.1.4:8443", "localhost:8443", "127.0.0.1:0"} {
		for _, args := range [][]string{{"doctor"}, {"doctor", "--fix"}} {
			var out, errs bytes.Buffer
			client := &fakeIncusClient{version: "7.0.1", httpsAddress: addr}
			code := Run(context.Background(), args, &out, &errs, client, &fakeConfigLoader{})
			if code != 1 || !strings.Contains(errs.String(), "unsafe or invalid") ||
				strings.Contains(out.String(), "listener: configured") || calls != 0 {
				t.Fatalf("addr=%q args=%v exit=%d calls=%d stdout=%q stderr=%q", addr, args, code, calls, out.String(), errs.String())
			}
		}
	}
}

func TestDoctorAcceptsConfiguredLoopbackWithoutMutation(t *testing.T) {
	previous := setIncusHTTPSAddress
	t.Cleanup(func() { setIncusHTTPSAddress = previous })
	calls := 0
	setIncusHTTPSAddress = func(context.Context, string) error {
		calls++
		return nil
	}
	for _, addr := range []string{"127.0.0.1:8443", "127.0.0.2:8443", "[::1]:8443"} {
		for _, args := range [][]string{{"doctor"}, {"doctor", "--fix"}} {
			var out, errs bytes.Buffer
			client := &fakeIncusClient{version: "7.0.1", httpsAddress: addr}
			code := Run(context.Background(), args, &out, &errs, client, &fakeConfigLoader{})
			if code != 0 || calls != 0 || !strings.Contains(out.String(), "listener: configured ("+addr+")") {
				t.Fatalf("addr=%q args=%v exit=%d calls=%d stdout=%q stderr=%q", addr, args, code, calls, out.String(), errs.String())
			}
		}
	}
}
