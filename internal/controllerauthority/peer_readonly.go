package controllerauthority

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
)

const peerEnrollmentSocket = "/run/aginctus-authority/peer-enrollment.sock"

// PeerEnrollmentService only reports whether the kernel-identified caller
// matches a fresh host-owned enrollment. It never accepts a caller-supplied
// UID, TLS fingerprint, owner policy path or write request. Its one positive
// response explicitly says NOT AUTHORIZED for Incus mutations.
//
// This is not the authenticated writer state service or a controller fence.
// A root administrator must independently provision the runtime directory.
// It is NOT installed, enabled or started by NixOS or Home Manager.
type PeerEnrollmentService struct {
	socket string
	original os.FileInfo
	listener net.Listener
	server *http.Server
	once sync.Once
	done chan struct{}
}

// StartPeerEnrollmentReadOnly requires a real root-owned runtime directory
// and the independently root-authored enrollment file. It listens only on
// one fixed AF_UNIX pathname and has no Incus client or mutation endpoint.
func StartPeerEnrollmentReadOnly(ctx context.Context) (*PeerEnrollmentService, error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("host peer-enrollment service requires root")
	}
	return startPeerEnrollmentAt(ctx, hostEnrollmentDir, peerEnrollmentSocket, 0)
}

// startPeerEnrollmentAt permits fixture paths solely inside this package's
// hermetic tests. Production never accepts an alternate directory/socket.
func startPeerEnrollmentAt(ctx context.Context, policyDir, socket string, owner uint32) (*PeerEnrollmentService, error) {
	if ctx == nil || !filepath.IsAbs(socket) {
		return nil, errors.New("peer-enrollment service needs context and absolute socket")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := loadOwnerFile(policyDir, owner); err != nil {
		return nil, err
	}
	parent := filepath.Dir(socket)
	fd, err := syscall.Open(parent, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	if err := validateOwnerFD(fd, owner, true); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	syscall.Close(fd)
	if _, err := os.Lstat(socket); err == nil {
		return nil, errors.New("existing authority socket cannot be adopted")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	created, err := os.Lstat(socket)
	if err != nil || created.Mode()&os.ModeSocket == 0 {
		_ = listener.Close()
		return nil, errors.New("authority socket provenance unavailable")
	}
	cleanup := func(err error) (*PeerEnrollmentService, error) {
		_ = listener.Close()
		_ = removePeerSocket(socket, created)
		return nil, err
	}
	// Future host bootstrap may assign a dedicated, limited socket group.
	// The handler still enforces SO_PEERCRED independently of filesystem mode.
	if err := os.Chmod(socket, 0o660); err != nil {
		return cleanup(err)
	}
	svc := &PeerEnrollmentService{
		socket: socket, original: created, listener: listener,
		done: make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/peer-enrollment", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "read-only", http.StatusMethodNotAllowed)
			return
		}
		uid, ok := r.Context().Value(authorityPeerUIDKey{}).(uint32)
		if !ok {
			http.Error(w, "unauthenticated peer", http.StatusForbidden)
			return
		}
		q := r.URL.Query()
		for _, key := range []string{"deployment", "project", "workload", "revision", "generation", "action"} {
			if len(q[key]) != 1 || q.Get(key) == "" {
				http.Error(w, "explicit selection required", http.StatusBadRequest)
				return
			}
		}
		if len(q) != 6 {
			http.Error(w, "unknown authority selector", http.StatusBadRequest)
			return
		}
		generation, err := strconv.ParseUint(q.Get("generation"), 10, 64)
		if err != nil || generation == 0 {
			http.Error(w, "invalid selected revision generation", http.StatusBadRequest)
			return
		}
		selection := SelectedRevision{
			DeploymentID: q.Get("deployment"), Project: q.Get("project"),
			WorkloadID: q.Get("workload"), RevisionID: q.Get("revision"),
			Generation: generation,
		}
		// Reload the owner-anchored file for EVERY request; a cached
		// enrollment must not silently outlive an explicit revocation.
		policy, err := loadOwnerFile(policyDir, owner)
		if err != nil {
			http.Error(w, "host authority unavailable", http.StatusServiceUnavailable)
			return
		}
		if !peerUIDEnrolled(policy, uid, selection, q.Get("action")) {
			http.Error(w, "not enrolled", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(struct {
			Code string `json:"code"`
		}{Code: "peer-uid-enrolled-not-authorized"})
	})
	svc.server = &http.Server{
		Handler: mux,
		ReadHeaderTimeout: 5*time.Second,
		IdleTimeout: 30*time.Second,
		MaxHeaderBytes: 16<<10,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			unix, ok := c.(*net.UnixConn)
			if !ok { return ctx }
			raw, err := unix.SyscallConn()
			if err != nil { return ctx }
			var cred *syscall.Ucred
			err = raw.Control(func(fd uintptr) {
				cred, err = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
			})
			if err != nil || cred == nil { return ctx }
			return context.WithValue(ctx, authorityPeerUIDKey{}, cred.Uid)
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

type authorityPeerUIDKey struct{}

// peerUIDEnrolled is deliberately weaker than Assess: it checks a kernel
// UID and exact selection, but does NOT claim to prove a matching Incus TLS
// certificate, accepted pin, host capability or exclusive live owner.
func peerUIDEnrolled(h *HostEvidence, uid uint32, selection SelectedRevision, action string) bool {
	if h == nil || !idPattern.MatchString(selection.DeploymentID) ||
		!idPattern.MatchString(selection.Project) || selection.Project == "default" ||
		!idPattern.MatchString(selection.WorkloadID) ||
		!hashPattern.MatchString(selection.RevisionID) || selection.Generation == 0 ||
		(action != "start" && action != "stop") {
		return false
	}
	for _, e := range h.entries {
		if e.DeploymentID != selection.DeploymentID ||
			e.IncusProject != selection.Project || e.WorkloadID != selection.WorkloadID {
			continue
		}
		if e.Status != "active" || e.UID != uid || e.RevisionID != selection.RevisionID ||
			e.Generation != selection.Generation { return false }
		for _, allowed := range e.Actions {
			if allowed == action { return true }
		}
		return false
	}
	return false
}

func (s *PeerEnrollmentService) Close() error {
	if s == nil { return nil }
	var result error
	s.once.Do(func() {
		defer close(s.done)
		result = s.server.Close()
		if err := removePeerSocket(s.socket, s.original); err != nil {
			result = err
		}
	})
	return result
}

func removePeerSocket(path string, original os.FileInfo) error {
	if current, err := os.Lstat(path); err == nil && os.SameFile(current, original) {
		return os.Remove(path)
	}
	return nil
}
