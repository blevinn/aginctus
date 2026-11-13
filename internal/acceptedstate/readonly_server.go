package acceptedstate

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// ReadOnlyUnixService is a same-UID, NON-PRIVILEGED read-only service.
// A future authenticated state-service API must not treat this as a
// delegation mechanism between root and an untrusted Home Manager user.
type ReadOnlyUnixService struct {
	socket   string
	origin   os.FileInfo
	listener net.Listener
	server   *http.Server
	once     sync.Once
	done     chan struct{}
}

// StartReadOnlyUnixService requires an already-private, owned parent directory.
// The socket never listens on TCP, has no mutation endpoint and refuses
// cross-UID connections through SO_PEERCRED even if mode bits are misconfigured.
func StartReadOnlyUnixService(ctx context.Context, store *LocalStore, socket string) (*ReadOnlyUnixService, error) {
	if ctx == nil || store == nil || !filepath.IsAbs(socket) {
		return nil, errors.New("service needs context, store and absolute socket path")
	}
	if err := privateStoreDirectory(filepath.Dir(socket)); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(socket); err == nil {
		return nil, errors.New("existing socket path must not be adopted")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	// Go's default UnixListener.Close unlinks by pathname without checking
	// inode identity. All cleanup must instead use our provenance check.
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	info, err := os.Lstat(socket)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		listener.Close()
		return nil, errors.New("unix listener identity unverified")
	}
	cleanup := func(e error) (*ReadOnlyUnixService, error) {
		listener.Close()
		removeOriginalSocket(socket, info)
		return nil, e
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		return cleanup(err)
	}
	svc := &ReadOnlyUnixService{socket: socket, origin: info, listener: listener, done: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/active", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "read-only endpoint", http.StatusMethodNotAllowed)
			return
		}
		deployment := r.URL.Query().Get("deployment")
		if deployment == "" || !localID.MatchString(deployment) {
			http.Error(w, "explicit deployment required", http.StatusBadRequest)
			return
		}
		rev, spec, err := store.ReadActive()
		if err != nil {
			http.Error(w, "accepted state unavailable", http.StatusServiceUnavailable)
			return
		}
		if rev == nil {
			http.Error(w, "no accepted revision", http.StatusNotFound)
			return
		}
		if spec.Selection.DeploymentID != deployment {
			http.Error(w, "deployment not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Revision Revision       `json:"revision"`
			Spec     DeploymentSpec `json:"spec"`
		}{*rev, *spec})
	})
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "read-only endpoint", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	svc.server = &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uid, ok := r.Context().Value(peerUIDKey{}).(uint32)
			if !ok || uid != uint32(os.Geteuid()) {
				http.Error(w, "unauthorized", http.StatusForbidden)
				return
			}
			mux.ServeHTTP(w, r)
		}),
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			unix, ok := c.(*net.UnixConn)
			if !ok {
				return ctx
			}
			raw, err := unix.SyscallConn()
			if err != nil {
				return ctx
			}
			var uid uint32
			var permitted bool
			err = raw.Control(func(fd uintptr) {
				cred, e := syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
				if e == nil {
					uid = cred.Uid
					permitted = true
				}
			})
			if err != nil || !permitted {
				return ctx
			}
			return context.WithValue(ctx, peerUIDKey{}, uid)
		},
	}
	go func() { _ = svc.server.Serve(listener) }()
	go func() {
		select {
		case <-ctx.Done():
			_ = svc.Close()
		case <-svc.done:
		}
	}()
	return svc, nil
}

type peerUIDKey struct{}

// Close does not remove a replaced/unowned path; it only unlinks the exact
// original socket inode and never deletes unknown foreign resources.
func (s *ReadOnlyUnixService) Close() error {
	if s == nil {
		return nil
	}
	var err error
	s.once.Do(func() {
		defer close(s.done)
		err = s.server.Close()
		if e := removeOriginalSocket(s.socket, s.origin); e != nil {
			err = e
		}
	})
	return err
}

func removeOriginalSocket(path string, origin os.FileInfo) error {
	if info, err := os.Lstat(path); err == nil && os.SameFile(info, origin) {
		return os.Remove(path)
	}
	return nil
}
