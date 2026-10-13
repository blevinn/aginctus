// Package oauthgateway contains the isolated, fail-closed service boundary for
// Aginctus-managed OAuth inference. Real token import and Responses forwarding
// are deliberately unavailable until the authenticated handoff is implemented.
package oauthgateway

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const StatePath = "/var/lib/aginctus/oauth"

// CheckStateDir verifies that the daemon is using a private, already-mounted
// credential directory. It never creates state or follows symlinks. A mount
// assertion in the guest's systemd unit provides the outer mountpoint check.
func CheckStateDir(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("OAuth state directory must be an absolute, normalized path")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect OAuth credential state: %w", err)
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("OAuth credential state must be an owner-only directory, not a symlink")
	}
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil {
		return err
	}
	if stat.Uid != uint32(os.Geteuid()) {
		return errors.New("OAuth credential state must be owned by the daemon user")
	}
	return nil
}

// Handler is safe to serve before a credential manager and provider adapter
// exist: neither client credentials nor upstream requests can be accepted.
// Liveness does not mean the gateway is ready for inference.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte("alive\n"))
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "OAuth inference unavailable: adapter and credential handoff not provisioned", http.StatusServiceUnavailable)
	})
	return mux
}

// ListenLoopback refuses to accept a non-loopback bind address. Later
// authenticated LiteLLM-to-adapter ingress will have a separate listener.
func ListenLoopback(address string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(host) == "" {
		return nil, errors.New("OAuth daemon must not listen on a wildcard interface")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("OAuth daemon accepts loopback bind addresses only")
	}
	return net.Listen("tcp", address)
}
