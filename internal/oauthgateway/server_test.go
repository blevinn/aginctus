package oauthgateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandlerFailsClosed(t *testing.T) {
	for _, path := range []string{"/v1/responses", "/v1/chat/completions", "/admin/import", "/readyz", "/"} {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader("TEST_CREDENTIAL_MUST_NOT_BE_LOGGED"))
		w := httptest.NewRecorder()
		Handler().ServeHTTP(w, r)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s returned %d", path, w.Code)
		}
		if strings.Contains(w.Body.String(), "TEST_CREDENTIAL_MUST_NOT_BE_LOGGED") {
			t.Fatal("reflected secret")
		}
	}
}
func TestLivenessNotReadiness(t *testing.T) {
	server := httptest.NewServer(Handler())
	defer server.Close()
	response, err := server.Client().Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("liveness endpoint not available")
	}
	response, err = server.Client().Get(server.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 503 {
		t.Fatal("unconfigured gateway incorrectly reports ready")
	}
}
func TestLoopbackOnly(t *testing.T) {
	for _, addr := range []string{":8080", "0.0.0.0:8080", "[::]:8080", "192.0.2.2:8080", "localhost:8080"} {
		l, err := ListenLoopback(addr)
		if err == nil {
			l.Close()
			t.Fatalf("unsafe listen address permitted: %q", addr)
		}
	}
	l, err := ListenLoopback("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l.Close()
}
func TestStateDirPermissionsAndSymlinks(t *testing.T) {
	path := t.TempDir()
	// Go's TempDir permissions can vary with parent and umask settings.
	// Establish the valid security precondition explicitly before testing it.
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := CheckStateDir(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := CheckStateDir(path); err == nil {
		t.Fatal("world-readable token directory allowed")
	}
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := CheckStateDir(path); err != nil {
		t.Fatalf("corrected permissions should pass: %v", err)
	}
	link := filepath.Join(t.TempDir(), "oauth")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := CheckStateDir(link); err == nil {
		t.Fatal("symlinked OAuth state was allowed")
	}
}
