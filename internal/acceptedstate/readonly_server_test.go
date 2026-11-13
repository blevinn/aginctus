package acceptedstate

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func socketClient(path string) *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		}},
	}
}

// shortUnixServiceTestStore avoids embedding long test names in socket paths.
// Linux's sockaddr_un.sun_path is only 108 bytes, and Nix CI may set a
// longer TMPDIR than the normal Go test environment. MkdirTemp still creates
// a unique, owner-private (0700) directory and cleanup remains test-scoped.
func shortUnixServiceTestStore(t *testing.T) (*LocalStore, string) {
	t.Helper()
	base, err := os.MkdirTemp("", "aginctus-sock-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(base); err != nil {
			t.Errorf("remove temporary socket directory: %v", err)
		}
	})
	root := filepath.Join(base, "state")
	store, err := InitializeLocalStore(root)
	if err != nil {
		t.Fatal(err)
	}
	return store, root
}

func TestReadOnlyUnixServicePreservesReplacement(t *testing.T) {
	store, root := shortUnixServiceTestStore(t)
	path := filepath.Join(root, "status.sock")
	svc, err := StartReadOnlyUnixService(context.Background(), store, path)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("socket not private", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "replacement" {
		t.Fatal("removed replacement path", err)
	}
	select {
	case <-svc.done:
	default:
		t.Fatal("close did not release context waiter")
	}
}

func TestReadOnlyUnixServiceRejectsMissingOrForeignPeer(t *testing.T) {
	store, root := shortUnixServiceTestStore(t)
	svc, err := StartReadOnlyUnixService(context.Background(), store, filepath.Join(root, "status.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	for _, ctx := range []context.Context{
		context.Background(),
		context.WithValue(context.Background(), peerUIDKey{}, uint32(os.Geteuid())+1),
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/health", nil).WithContext(ctx)
		svc.server.Handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("unauthenticated peer got %d", response.Code)
		}
	}
}

func TestReadOnlyUnixServiceRestart(t *testing.T) {
	store, root := shortUnixServiceTestStore(t)
	accepted, err := store.CommitLocal(0, "", validSpec())
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(root, "status.sock")
	ctx, cancel := context.WithCancel(context.Background())
	svc, err := StartReadOnlyUnixService(ctx, store, socket)
	if err != nil {
		t.Fatal(err)
	}
	client := socketClient(socket)
	check := func(method, url string, want int) {
		t.Helper()
		req, err := http.NewRequest(method, "http://unix"+url, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("%s %s got %d want %d", method, url, res.StatusCode, want)
		}
		if method == "GET" && want == http.StatusOK {
			var doc struct {
				Revision Revision
				Spec     DeploymentSpec
			}
			if err := json.NewDecoder(res.Body).Decode(&doc); err != nil {
				t.Fatal(err)
			}
			if doc.Revision.ID != accepted.ID || doc.Spec.Selection.DeploymentID != "demo" {
				t.Fatal("wrong accepted revision")
			}
		}
	}
	check("GET", "/v1/health", 204)
	check("GET", "/v1/active?deployment=demo", 200)
	check("GET", "/v1/active?deployment=foreign", 404)
	check("GET", "/v1/active", 400)
	check("POST", "/v1/active?deployment=demo", 405)
	cancel()
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenLocalStore(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	next, err := StartReadOnlyUnixService(ctx2, reopened, socket)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	check("GET", "/v1/active?deployment=demo", 200)
}

func TestReadOnlyUnixServiceRejectsUnsafeSocketPath(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "state")
	store, err := InitializeLocalStore(root)
	if err != nil {
		t.Fatal(err)
	}
	// t.TempDir is private by default; explicitly make the socket parent
	// unsafe while keeping the already-initialized state directory private.
	if err := os.Chmod(base, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := StartReadOnlyUnixService(ctx, store, filepath.Join(base, "exposed.sock")); err == nil {
		t.Fatal("accepted nonprivate socket parent")
	}
	path := filepath.Join(root, "status.sock")
	if err := os.WriteFile(path, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := StartReadOnlyUnixService(ctx, store, path); err == nil {
		t.Fatal("adopted existing path")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "foreign" {
		t.Fatal("modified foreign path")
	}
}
