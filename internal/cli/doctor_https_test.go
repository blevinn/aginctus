package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestDoctorHTTPSRequiresExplicitLoopbackAndPort(t *testing.T) {
	cases := []struct {
		name string
		addr string
		ok   bool
	}{
		{"ipv4 loopback", "127.0.0.1:8443", true},
		{"alternate ipv4 loopback", "127.12.34.56:1234", true},
		{"ipv6 loopback", "[::1]:8443", true},
		{"last valid port", "[::1]:65535", true},
		{"ipv4 wildcard", "0.0.0.0:8443", false},
		{"ipv6 wildcard", "[::]:8443", false},
		{"all interfaces shorthand", ":8443", false},
		{"private LAN", "192.168.1.10:8443", false},
		{"link local", "[fe80::1]:8443", false},
		{"public address", "8.8.8.8:8443", false},
		{"hostname", "localhost:8443", false},
		{"trailing whitespace", "127.0.0.1:8443 ", false},
		{"control characters", "127.0.0.1:8443\nunsafe", false},
		{"missing port", "127.0.0.1", false},
		{"zero port", "127.0.0.1:0", false},
		{"out of range port", "127.0.0.1:65536", false},
		{"nonnumeric port", "127.0.0.1:http", false},
		{"signed port", "127.0.0.1:+8443", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isLoopbackHTTPSAddress(tc.addr); got != tc.ok {
				t.Fatalf("isLoopbackHTTPSAddress(%q)=%t, want %t", tc.addr, got, tc.ok)
			}
		})
	}
}

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
