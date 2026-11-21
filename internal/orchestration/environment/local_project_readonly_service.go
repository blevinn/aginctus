package environment

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

// LocalProjectReadOnlyService is a same-UID, nonprivileged, TEST-ONLY view of
// the environment-state volume fixture. It does not authenticate an Incus
// principal, provide remote access, accept revisions or execute workloads.
type LocalProjectReadOnlyService struct {
	server *http.Server
	listener net.Listener
	socket string
	socketInfo os.FileInfo
	closed chan struct{}
	once sync.Once
}

type fixturePeerUIDKey struct{}

func StartLocalProjectReadOnly(ctx context.Context, store *LocalProjectStateStore, socket string) (*LocalProjectReadOnlyService, error) {
	if ctx == nil || store == nil || !filepath.IsAbs(socket) || filepath.Clean(socket) != socket {
		return nil, errors.New("local project service requires a store and absolute socket")
	}
	if err := ctx.Err(); err != nil { return nil, err }
	if err := checkPrivateLocalDir(filepath.Dir(socket)); err != nil { return nil,err }
	// No adoption, unlink or replacement of an existing socket path.
	if _, err := os.Lstat(socket); err == nil {
		return nil, errors.New("local project socket already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil { return nil,err }
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	info, err := os.Lstat(socket)
	if err != nil || !info.Mode().IsRegular() && info.Mode()&os.ModeSocket == 0 {
		_ = listener.Close()
		return nil, errors.New("local project socket identity unavailable")
	}
	cleanup := func(err error) (*LocalProjectReadOnlyService, error) {
		_ = listener.Close()
		_ = removeLocalProjectSocket(socket, info)
		return nil,err
	}
	if err := os.Chmod(socket, 0o600); err != nil {return cleanup(err)}
	svc := &LocalProjectReadOnlyService{
		listener:listener, socket:socket, socketInfo:info, closed:make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "read-only", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.RawQuery != "" {
			http.Error(w, "unexpected selector", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/v1/accepted", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "read-only", http.StatusMethodNotAllowed)
			return
		}
		q := r.URL.Query()
		ids,ok:=q["environment"]
		if !ok || len(q)!=1 || len(ids)!=1 || !idPattern.MatchString(ids[0]) {
			http.Error(w, "exact immutable environment selection required", http.StatusBadRequest)
			return
		}
		desc := Descriptor{ID:ids[0],Name:"fixture-read",Binding:store.binding}
		rev,candidate,err:=store.ReadLocalAccepted(r.Context(),desc)
		if err!=nil {
			http.Error(w, "local accepted state unavailable", http.StatusServiceUnavailable)
			return
		}
		if rev==nil {
			// The fixture can report a missing record but cannot prove that
			// this UUID was enrolled or authorize its creation.
			http.Error(w, "local accepted revision absent", http.StatusNotFound)
			return
		}
		if candidate==nil || candidate.Environment.ID!=desc.ID {
			http.Error(w,"inconsistent accepted history",http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct{
			EnvironmentID string `json:"environmentId"`
			Revision AcceptedRevision `json:"revision"`
		}{desc.ID,*rev})
	})
	svc.server=&http.Server{
		Handler:http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
			uid,ok:=r.Context().Value(fixturePeerUIDKey{}).(uint32)
			if !ok || uid!=uint32(os.Geteuid()){
				http.Error(w,"not authorized",http.StatusForbidden)
				return
			}
			mux.ServeHTTP(w,r)
		}),
		ReadHeaderTimeout:5*time.Second, IdleTimeout:30*time.Second,
		MaxHeaderBytes:16<<10,
		ConnContext:func(ctx context.Context,conn net.Conn) context.Context{
			unix,ok:=conn.(*net.UnixConn)
			if !ok{return ctx}
			raw,err:=unix.SyscallConn()
			if err!=nil{return ctx}
			var uid uint32
			var found bool
			controlErr:=raw.Control(func(fd uintptr){
				peer,peerErr:=syscall.GetsockoptUcred(int(fd),syscall.SOL_SOCKET,syscall.SO_PEERCRED)
				if peerErr==nil{uid=peer.Uid;found=true}
			})
			if controlErr!=nil || !found{return ctx}
			return context.WithValue(ctx,fixturePeerUIDKey{},uid)
		},
	}
	go func(){_ = svc.server.Serve(listener)}()
	go func(){
		select{
		case <-ctx.Done():_ = svc.Close()
		case <-svc.closed:
		}
	}()
	return svc,nil
}

func removeLocalProjectSocket(path string, original os.FileInfo) error{
	if current,err:=os.Lstat(path);err==nil && os.SameFile(current,original){
		return os.Remove(path)
	}
	return nil
}

// Close refuses to remove a socket name that another actor replaced, and
// does not touch Incus volumes, instances, revisions or runtime secrets.
func (s *LocalProjectReadOnlyService) Close() error{
	if s==nil{return nil}
	var closeErr error
	s.once.Do(func(){
		closeErr=s.server.Close()
		if err:=removeLocalProjectSocket(s.socket,s.socketInfo);err!=nil{closeErr=err}
		close(s.closed)
	})
	return closeErr
}
