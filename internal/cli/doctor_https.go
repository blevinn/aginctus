package cli

import (
	"net"
	"strconv"
)

// isLoopbackHTTPSAddress verifies the configured Incus bind address itself,
// without resolving DNS or inferring that a hostname is safe. An explicit
// loopback IP and valid TCP port are required; an empty host binds all
// interfaces in many server configurations and is never acceptable here.
func isLoopbackHTTPSAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() || port == "" {
		return false
	}
	for _, digit := range port {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	number, err := strconv.Atoi(port)
	return err == nil && number > 0 && number <= 65535
}
