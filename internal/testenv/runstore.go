package testenv

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	runIDPrefix          = "vm-"
	runSchemaVersion     = 1
	guestAgentSocketName = "qga.sock"
	maxRunLifetime       = 2 * time.Hour
)

// RunStore holds process ownership records. This must be opened from a
// trusted supervisor configuration on a dedicated worker identity. A store
// writable by the agent itself is not an isolation boundary.
type RunStore struct{ root string }

type RunRecord struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"id"`
	OwnerUID      int       `json:"owner_uid"`
	PID           int       `json:"pid"`
	ProcessStart  string    `json:"process_start_ticks"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type RunStatus struct {
	ID        string    `json:"id"`
	Running   bool      `json:"running"`
	ExpiresAt time.Time `json:"expires_at"`
	Reason    string    `json:"reason,omitempty"`
}

func validRunID(id string) bool {
	if !strings.HasPrefix(id, runIDPrefix) || len(id) != len(runIDPrefix)+24 {
		return false
	}
	for _, c := range id[len(runIDPrefix):] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// OpenRunStore rejects symlinked roots and roots accessible to other UIDs.
// The caller must select the root independently of untrusted tool arguments.
func OpenRunStore(root string) (*RunStore, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, errors.New("run store root must be an absolute clean path")
	}
	// Linux AF_UNIX sun_path fits at most 107 non-NUL pathname bytes.
	if len(filepath.Join(root, runIDPrefix+strings.Repeat("a", 24), guestAgentSocketName)) >= 108 {
		return nil, errors.New("run store root too long for Unix guest agent socket")
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return nil, errors.New("run store must be an existing private 0700 directory")
	}
	if err := ownedByWorker(info); err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || resolved != root {
		return nil, errors.New("symlinks in run store root are not permitted")
	}
	return &RunStore{root: root}, nil
}

func ownedByWorker(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return errors.New("run store resource is not owned by worker")
	}
	return nil
}

// processIdentity pins a process to its Linux creation epoch, so a reused
// PID is never accepted as the process registered for a VM run.
func processIdentity(pid int) (string, error) {
	if pid <= 0 {
		return "", errors.New("invalid QEMU PID")
	}
	path := filepath.Join("/proc", strconv.Itoa(pid))
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if err := ownedByWorker(info); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(path, "stat"))
	if err != nil {
		return "", err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return "", errors.New("invalid process stat")
	}
	fields := strings.Fields(string(data[end+1:]))
	// Field 22 (starttime) has index 19 after the name+PID delimiter.
	if len(fields) <= 19 {
		return "", errors.New("invalid process stat length")
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return "", errors.New("invalid process start time")
	}
	return fields[19], nil
}

// RegisterRun must be called only by the trusted supervisor after starting
// QEMU. This method alone does not launch or authenticate a QEMU process.
func (s *RunStore) RegisterRun(pid int, ttl time.Duration) (RunRecord, error) {
	if ttl <= 0 || ttl > maxRunLifetime {
		return RunRecord{}, errors.New("invalid VM lifetime")
	}
	start, err := processIdentity(pid)
	if err != nil {
		return RunRecord{}, fmt.Errorf("cannot bind VM owner process: %w", err)
	}
	var token [12]byte
	if _, err := rand.Read(token[:]); err != nil {
		return RunRecord{}, err
	}
	now := time.Now().UTC()
	rec := RunRecord{
		SchemaVersion: runSchemaVersion,
		ID:            runIDPrefix + hex.EncodeToString(token[:]),
		OwnerUID:      os.Getuid(),
		PID:           pid,
		ProcessStart:  start,
		CreatedAt:     now,
		ExpiresAt:     now.Add(ttl),
	}
	dir := filepath.Join(s.root, rec.ID)
	if err := os.Mkdir(dir, 0700); err != nil {
		return RunRecord{}, err
	}
	file, err := os.OpenFile(filepath.Join(dir, "run.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		_ = os.Remove(dir)
		return RunRecord{}, err
	}
	writeErr := json.NewEncoder(file).Encode(rec)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return RunRecord{}, errors.New("cannot write VM ownership record; inspect private run directory")
	}
	return rec, nil
}

func (s *RunStore) ReadRun(id string) (RunRecord, error) {
	if !validRunID(id) {
		return RunRecord{}, errors.New("invalid VM run ID")
	}
	dir := filepath.Join(s.root, id)
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return RunRecord{}, errors.New("run directory missing or unsafe")
	}
	if err := ownedByWorker(info); err != nil {
		return RunRecord{}, err
	}
	path := filepath.Join(dir, "run.json")
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm() != 0600 {
		return RunRecord{}, errors.New("VM ownership record missing or unsafe")
	}
	if err := ownedByWorker(fi); err != nil {
		return RunRecord{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return RunRecord{}, err
	}
	defer file.Close()
	var rec RunRecord
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rec); err != nil {
		return RunRecord{}, errors.New("malformed VM ownership record")
	}
	if rec.SchemaVersion != runSchemaVersion || rec.ID != id || rec.OwnerUID != os.Getuid() || rec.PID <= 0 ||
		rec.ProcessStart == "" || rec.ExpiresAt.Sub(rec.CreatedAt) > maxRunLifetime || !rec.ExpiresAt.After(rec.CreatedAt) {
		return RunRecord{}, errors.New("VM ownership record conflicts with caller")
	}
	return rec, nil
}

func (s *RunStore) Status(id string) (RunStatus, error) {
	rec, err := s.ReadRun(id)
	if err != nil {
		return RunStatus{}, err
	}
	status := RunStatus{ID: id, ExpiresAt: rec.ExpiresAt}
	if !time.Now().Before(rec.ExpiresAt) {
		status.Reason = "expired"
		return status, nil
	}
	current, err := processIdentity(rec.PID)
	if err != nil || current != rec.ProcessStart {
		status.Reason = "process missing or changed"
		return status, nil
	}
	status.Running = true
	return status, nil
}

// ExecGuest derives the socket from trusted run ownership state; the MCP
// caller must never select a host socket or supply a filesystem path.
func (s *RunStore) ExecGuest(ctx context.Context, id string, argv []string, timeout time.Duration) (GuestResult, error) {
	status, err := s.Status(id)
	if err != nil {
		return GuestResult{}, err
	}
	if !status.Running {
		return GuestResult{}, fmt.Errorf("VM run unavailable: %s", status.Reason)
	}
	path := filepath.Join(s.root, id, guestAgentSocketName)
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 {
		return GuestResult{}, errors.New("owned guest socket unavailable")
	}
	if err := ownedByWorker(info); err != nil {
		return GuestResult{}, err
	}
	return (GuestAgent{SocketPath: path}).Exec(ctx, argv, timeout)
}
