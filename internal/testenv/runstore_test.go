package testenv

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunStoreOwnershipAndStaleProcess(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runs")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRunStore(root)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := store.RegisterRun(os.Getpid(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !validRunID(rec.ID) {
		t.Fatalf("invalid generated ID %q", rec.ID)
	}
	st, err := store.Status(rec.ID)
	if err != nil || !st.Running {
		t.Fatalf("not running: %#v, %v", st, err)
	}
	if _, err := store.Status("../../etc/passwd"); err == nil {
		t.Fatal("accepted traversal")
	}
	if _, err := store.RegisterRun(0, time.Minute); err == nil {
		t.Fatal("accepted PID 0")
	}
	if _, err := store.RegisterRun(os.Getpid(), 3*time.Hour); err == nil {
		t.Fatal("accepted unbounded lifetime")
	}
	path := filepath.Join(root, rec.ID, "run.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(data), rec.ProcessStart, "0")), 0600); err != nil {
		t.Fatal(err)
	}
	st, err = store.Status(rec.ID)
	if err != nil || st.Running || st.Reason != "process missing or changed" {
		t.Fatalf("stale run treated alive: %#v, %v", st, err)
	}
	if _, err := store.ExecGuest(context.Background(), rec.ID, []string{"/bin/true"}, time.Second); err == nil {
		t.Fatal("executed stale run")
	}
}

func TestRunStoreRejectsUnsafeRootAndManifest(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "ax-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	bad := filepath.Join(root, "world-readable")
	if err := os.Mkdir(bad, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenRunStore(bad); err == nil {
		t.Fatal("accepted unsafe root")
	}
	private := filepath.Join(root, "private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "symlink")
	if err := os.Symlink(private, link); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenRunStore(link); err == nil {
		t.Fatal("accepted symlink root")
	}
	store, err := OpenRunStore(private)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := store.RegisterRun(os.Getpid(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(private, rec.ID, "run.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadRun(rec.ID); err == nil {
		t.Fatal("accepted symlink manifest")
	}
}

func TestRunStoreRefusesMissingGuestSocket(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runs")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRunStore(root)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := store.RegisterRun(os.Getpid(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ExecGuest(context.Background(), rec.ID, []string{"/bin/true"}, time.Second); err == nil || !strings.Contains(err.Error(), "owned guest socket unavailable") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestOwnedRunExecRoutesOnlyThroughOwnedQGASocket(t *testing.T) {
	short, err := os.MkdirTemp("/tmp", "ax-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	root := filepath.Join(short, "runs")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRunStore(root)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := store.RegisterRun(os.Getpid(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(root, rec.ID, guestAgentSocketName)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewScanner(conn)
		for reader.Scan() {
			var request map[string]json.RawMessage
			if json.Unmarshal(reader.Bytes(), &request) != nil {
				return
			}
			var name string
			_ = json.Unmarshal(request["execute"], &name)
			response := map[string]any{"return": map[string]any{}}
			if name == "guest-exec" {
				response["return"] = map[string]any{"pid": 42}
			}
			if name == "guest-exec-status" {
				response["return"] = map[string]any{"exited": true, "exitcode": 0, "out-data": base64.StdEncoding.EncodeToString([]byte("hello from isolated guest"))}
			}
			payload, _ := json.Marshal(response)
			if _, err := conn.Write(append(payload, '\n')); err != nil {
				return
			}
		}
	}()
	result, err := store.ExecGuest(context.Background(), rec.ID, []string{"/bin/true", "; rm -rf /host"}, time.Second)
	if err != nil || result.Stdout != "hello from isolated guest" {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}
