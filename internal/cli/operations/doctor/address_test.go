package doctor

import "testing"

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
			if got := IsLoopbackHTTPSAddress(tc.addr); got != tc.ok {
				t.Fatalf("IsLoopbackHTTPSAddress(%q)=%t, want %t", tc.addr, got, tc.ok)
			}
		})
	}
}

