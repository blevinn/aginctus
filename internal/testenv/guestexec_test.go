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

func fakeGuestAgent(t *testing.T, handler func(map[string]json.RawMessage) any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "qga.sock")
	listener, err := net.Listen("unix", path)
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
			var req map[string]json.RawMessage
			if json.Unmarshal(reader.Bytes(), &req) != nil {
				return
			}
			response, err := json.Marshal(handler(req))
			if err != nil {
				return
			}
			if _, err := conn.Write(append(response, '\n')); err != nil {
				return
			}
		}
	}()
	return path
}

func TestGuestAgentExecPreservesArgvAndCapturesOutput(t *testing.T) {
	var gotArgs []string
	calls := 0
	path := fakeGuestAgent(t, func(req map[string]json.RawMessage) any {
		calls++
		var op string
		_ = json.Unmarshal(req["execute"], &op)
		switch op {
		case "guest-ping":
			return map[string]any{"return": map[string]any{}}
		case "guest-exec":
			var args struct {
				Path string `json:"path"`
				Arg []string `json:"arg"`
				Capture bool `json:"capture-output"`
			}
			_ = json.Unmarshal(req["arguments"], &args)
			if args.Path != "/run/current-system/sw/bin/timeout" || !args.Capture {
				t.Errorf("bad path/capture: %#v", args)
			}
			gotArgs = args.Arg
			return map[string]any{"return": map[string]int{"pid": 42}}
		case "guest-exec-status":
			return map[string]any{"return": map[string]any{"exited": true, "exitcode": 0, "out-data": base64.StdEncoding.EncodeToString([]byte("guest-only")), "err-data": ""}}
		default:
			t.Errorf("unexpected guest command %q", op)
			return map[string]any{"error": map[string]string{"class": "CommandNotFound"}}
		}
	})
	res, err := (GuestAgent{SocketPath: path}).Exec(context.Background(), []string{"/run/current-system/sw/bin/incus", "info", "; touch /host/leak", "$(id)"}, 2*time.Second)
	if err != nil || res.ExitCode != 0 || res.Stdout != "guest-only" || calls != 3 {
		t.Fatalf("result=%#v err=%v calls=%d", res, err, calls)
	}
	if strings.Join(gotArgs, "|") != "--signal=KILL|--kill-after=1s|2s|--|/run/current-system/sw/bin/incus|info|; touch /host/leak|$(id)" {
		t.Fatalf("guest argv was modified: %#v", gotArgs)
	}
}

func TestGuestAgentRejectsUnsafeInputs(t *testing.T) {
	agent := GuestAgent{SocketPath: "/nonexistent/qga.sock"}
	for _, args := range [][]string{nil, {"relative"}, {"/usr/bin/../bin/sh"}, {"/bin/sh", "bad\x00arg"}, {"/bin/sh", strings.Repeat("x", 9000)}} {
		if _, err := agent.Exec(context.Background(), args, time.Second); err == nil {
			t.Fatalf("accepted arguments: %#v", args)
		}
	}
	if _, err := agent.Exec(context.Background(), []string{"/bin/true"}, 0); err == nil {
		t.Fatal("accepted zero timeout")
	}
	if _, err := agent.Exec(context.Background(), []string{"/bin/true"}, time.Minute); err == nil {
		t.Fatal("accepted unbounded timeout")
	}
	link := filepath.Join(t.TempDir(), "qga.sock")
	if err := os.Symlink("/nonexistent", link); err != nil {
		t.Fatal(err)
	}
	if _, err := (GuestAgent{SocketPath: link}).Exec(context.Background(), []string{"/bin/true"}, time.Second); err == nil {
		t.Fatal("accepted socket symlink")
	}
}

func TestGuestAgentSuppressesGuestErrors(t *testing.T) {
	path := fakeGuestAgent(t, func(map[string]json.RawMessage) any {
		return map[string]any{"error": map[string]string{"class": "Denied", "desc": "token=PRIVATE_SECRET"}}
	})
	_, err := (GuestAgent{SocketPath: path}).Exec(context.Background(), []string{"/bin/true"}, time.Second)
	if err == nil || strings.Contains(err.Error(), "PRIVATE_SECRET") {
		t.Fatalf("error leaked: %v", err)
	}
}

func TestGuestAgentOutputBound(t *testing.T) {
	path := fakeGuestAgent(t, func(req map[string]json.RawMessage) any {
		var op string
		_ = json.Unmarshal(req["execute"], &op)
		switch op {
		case "guest-ping":
			return map[string]any{"return": map[string]any{}}
		case "guest-exec":
			return map[string]any{"return": map[string]int{"pid": 1}}
		default:
			return map[string]any{"return": map[string]any{"exited": true, "out-data": base64.StdEncoding.EncodeToString(make([]byte, guestExecMaxBytes+1))}}
		}
	})
	_, err := (GuestAgent{SocketPath: path}).Exec(context.Background(), []string{"/bin/true"}, time.Second)
	if err == nil || !strings.Contains(err.Error(), "output exceeded limit") {
		t.Fatalf("error=%v", err)
	}
}
