package testenv

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	guestExecMaxRuntime   = 30 * time.Second
	guestExecMaxBytes     = 64 * 1024
	guestAgentMaxResponse = 256 * 1024
	guestExecMaxArgs      = 32
	guestExecMaxArgBytes  = 8192
)

// GuestResult describes a command run INSIDE the guest by QEMU guest-exec.
// Nonzero guest exit status is data; transport failures return an error.
type GuestResult struct {
	ExitCode int    `json:"exit_code"`
	Signal   int    `json:"signal,omitempty"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// GuestAgent is a transport, not an authorization boundary. The socket
// must be obtained from trusted run ownership state, never from MCP input.
type GuestAgent struct{ SocketPath string }

type guestAgentResponse struct {
	Return json.RawMessage `json:"return"`
	Error  *struct {
		Class string `json:"class"`
		Desc  string `json:"desc"`
	} `json:"error"`
}

func validateGuestArgs(argv []string) error {
	if len(argv) == 0 || len(argv) > guestExecMaxArgs {
		return errors.New("guest command requires 1–32 arguments")
	}
	if !filepath.IsAbs(argv[0]) || filepath.Clean(argv[0]) != argv[0] {
		return errors.New("guest executable must be a clean absolute path")
	}
	total := 0
	for _, arg := range argv {
		if strings.IndexByte(arg, 0) >= 0 {
			return errors.New("guest argument contains NUL")
		}
		total += len(arg)
	}
	if total > guestExecMaxArgBytes {
		return errors.New("guest command arguments exceed limit")
	}
	return nil
}

// Exec uses guest-exec with argv as JSON, never a host shell. A guest-side
// timeout wrapper bounds typical children; VM TTL/destruction must provide
// the ultimate bound if QGA fails or the guest is compromised.
func (a GuestAgent) Exec(ctx context.Context, argv []string, limit time.Duration) (GuestResult, error) {
	if err := validateGuestArgs(argv); err != nil {
		return GuestResult{}, err
	}
	if limit <= 0 || limit > guestExecMaxRuntime {
		return GuestResult{}, errors.New("guest execution timeout must be 1–30 seconds")
	}
	if !filepath.IsAbs(a.SocketPath) || filepath.Clean(a.SocketPath) != a.SocketPath {
		return GuestResult{}, errors.New("guest agent socket path must be absolute and clean")
	}
	info, err := os.Lstat(a.SocketPath)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 {
		return GuestResult{}, errors.New("guest agent socket is unavailable or not a socket")
	}
	ctx, cancel := context.WithTimeout(ctx, limit+3*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", a.SocketPath)
	if err != nil {
		return GuestResult{}, fmt.Errorf("guest agent unavailable: %w", err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	reader := bufio.NewReaderSize(conn, guestAgentMaxResponse+1)
	call := func(command string, args any, result any) error {
		if deadline, ok := ctx.Deadline(); ok {
			if err := conn.SetDeadline(deadline); err != nil {
				return err
			}
		}
		request := map[string]any{"execute": command}
		if args != nil {
			request["arguments"] = args
		}
		encoded, err := json.Marshal(request)
		if err != nil {
			return err
		}
		if _, err := conn.Write(append(encoded, '\n')); err != nil {
			return fmt.Errorf("guest agent request failed: %w", err)
		}
		line, err := reader.ReadSlice('\n')
		if err != nil {
			return fmt.Errorf("guest agent response missing or oversized: %w", err)
		}
		var response guestAgentResponse
		if err := json.Unmarshal(line, &response); err != nil {
			return errors.New("malformed guest agent response")
		}
		if response.Error != nil {
			// Guest-controlled descriptions may include secrets; do not print.
			return fmt.Errorf("guest agent rejected %s", command)
		}
		if len(response.Return) == 0 {
			return errors.New("guest agent response lacks result")
		}
		return json.Unmarshal(response.Return, result)
	}
	var ping map[string]any
	if err := call("guest-ping", nil, &ping); err != nil {
		return GuestResult{}, err
	}
	seconds := int((limit + time.Second - 1) / time.Second)
	wrapped := append([]string{"--signal=KILL", "--kill-after=1s", strconv.Itoa(seconds) + "s", "--"}, argv...)
	var started struct {
		PID int `json:"pid"`
	}
	if err := call("guest-exec", map[string]any{
		"path": "/run/current-system/sw/bin/timeout", "arg": wrapped, "capture-output": true,
	}, &started); err != nil {
		return GuestResult{}, err
	}
	if started.PID <= 0 {
		return GuestResult{}, errors.New("guest agent returned invalid pid")
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		var status struct {
			Exited   bool   `json:"exited"`
			ExitCode int    `json:"exitcode"`
			Signal   int    `json:"signal"`
			OutData  string `json:"out-data"`
			ErrData  string `json:"err-data"`
		}
		if err := call("guest-exec-status", map[string]int{"pid": started.PID}, &status); err != nil {
			return GuestResult{}, err
		}
		if status.Exited {
			if len(status.OutData)+len(status.ErrData) > base64.StdEncoding.EncodedLen(guestExecMaxBytes)*2 {
				return GuestResult{}, errors.New("guest command output exceeded limit")
			}
			stdout, err := base64.StdEncoding.DecodeString(status.OutData)
			if err != nil {
				return GuestResult{}, errors.New("invalid guest stdout encoding")
			}
			stderr, err := base64.StdEncoding.DecodeString(status.ErrData)
			if err != nil {
				return GuestResult{}, errors.New("invalid guest stderr encoding")
			}
			if len(stdout)+len(stderr) > guestExecMaxBytes {
				return GuestResult{}, errors.New("guest command output exceeded limit")
			}
			return GuestResult{ExitCode: status.ExitCode, Signal: status.Signal, Stdout: string(bytes.Clone(stdout)), Stderr: string(bytes.Clone(stderr))}, nil
		}
		select {
		case <-ctx.Done():
			return GuestResult{}, fmt.Errorf("guest command timed out or cancelled: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
