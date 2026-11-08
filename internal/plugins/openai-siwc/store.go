// Package openaisiwc implements the local OpenAI SIWC enrollment and administration lifecycle.
// Its protected state and flock protocol are shared with the LiteLLM extension.
package openaisiwc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const maxState = 2 << 20

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
var clientPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)
var modelPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Error contains a fixed credential-free diagnostic suitable for CLI output.
type Error string

func (e Error) Error() string { return string(e) }

func ValidName(value string) bool { return namePattern.MatchString(value) }

type Record struct {
	ClientID          string   `json:"client_id"`
	Subject           string   `json:"sub,omitempty"`
	Email             string   `json:"email,omitempty"`
	AccessToken       string   `json:"access_token,omitempty"`
	RefreshToken      string   `json:"refresh_token,omitempty"`
	IDToken           string   `json:"id_token,omitempty"`
	Scopes            []string `json:"scopes"`
	ExpiresAt         float64  `json:"expires_at,omitempty"`
	EarliestRefreshAt float64  `json:"earliest_refresh_at,omitempty"`
	RefreshPending    bool     `json:"refresh_pending,omitempty"`
}

type Route struct {
	Account             string   `json:"account"`
	ClientID            string   `json:"client_id"`
	Subject             string   `json:"sub"`
	Model               string   `json:"model"`
	KeyHashes           []string `json:"key_hashes"`
	ConsentBackground   bool     `json:"consent_background"`
	ConsentConnectedApp bool     `json:"consent_connected_app"`
}

type State struct {
	Version  int                `json:"version"`
	HostID   string             `json:"host_id"`
	Accounts map[string]*Record `json:"accounts"`
	Routes   map[string]Route   `json:"routes"`
	Received []string           `json:"received,omitempty"`
	Consents map[string]Consent `json:"consents,omitempty"`
}

type Consent struct {
	ClientID string `json:"client_id"`
	Subject  string `json:"sub"`
}

type Store struct {
	Root   string
	memory []byte
}

func OpenStore(root string) (*Store, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, Error("SIWC state directory must be absolute and normalized")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	var stat unix.Stat_t
	if unix.Lstat(root, &stat) != nil || !info.IsDir() || stat.Mode&07777 != 0700 || stat.Uid != uint32(os.Geteuid()) {
		return nil, Error("SIWC state directory must be owned, non-symlink and mode 0700")
	}
	return &Store{Root: root}, nil
}

func safeFile(file *os.File) error {
	var stat unix.Stat_t
	if unix.Fstat(int(file.Fd()), &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&07777 != 0600 || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 {
		return Error("SIWC files must be owned, single-link regular files with mode 0600")
	}
	return nil
}

func privateRead(path string, limit int64) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	if err := safeFile(file); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err == nil && int64(len(data)) > limit {
		err = Error("SIWC file exceeds size limit")
	}
	return data, err
}

func (s *Store) lock(ctx context.Context, name string, timeout time.Duration) (func(), error) {
	// A managed exchange is single-use and holds the gateway's admission lock.
	if s.memory != nil {
		return func() {}, ctx.Err()
	}
	path := filepath.Join(s.Root, name+".flock")
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	closeFile := func() { _ = file.Close() }
	if err := safeFile(file); err != nil {
		closeFile()
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			closeFile()
			return nil, err
		}
		if !time.Now().Before(deadline) {
			closeFile()
			return nil, Error("SIWC owner busy; stop LiteLLM before enrollment, transfer or logout")
		}
		select {
		case <-ctx.Done():
			closeFile()
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	before, err := file.Stat()
	after, other := os.Lstat(path)
	if err != nil || other != nil || !os.SameFile(before, after) {
		closeFile()
		return nil, Error("SIWC lock was replaced")
	}
	return closeFile, nil
}

func validHost(id string) bool {
	if len(id) != 45 || id[:9] != "urn:uuid:" {
		return false
	}
	_, err := uuid.Parse(id[9:])
	return err == nil
}

func (state *State) validate() error {
	if state.Version != 1 || !validHost(state.HostID) || state.Accounts == nil || state.Routes == nil {
		return Error("Invalid SIWC state format")
	}
	for label, record := range state.Accounts {
		if !ValidName(label) || record == nil || !clientPattern.MatchString(record.ClientID) || record.ClientID == "dynamic_agent_client" {
			return Error("Invalid SIWC account registration")
		}
		if record.AccessToken != "" || record.RefreshToken != "" {
			if record.Subject == "" || record.AccessToken == "" || record.RefreshToken == "" || record.Scopes == nil || record.ExpiresAt <= 0 {
				return Error("Invalid persisted SIWC credentials; reauthorize")
			}
		}
	}
	for alias, route := range state.Routes {
		record := state.Accounts[route.Account]
		if !ValidName(alias) || len(alias) < 5 || alias[:5] != "siwc-" || record == nil || !modelPattern.MatchString(route.Model) || len(route.KeyHashes) == 0 || !route.ConsentBackground || !route.ConsentConnectedApp || route.ClientID != record.ClientID || route.Subject != record.Subject {
			return Error("Invalid persisted SIWC route binding")
		}
	}
	for label, consent := range state.Consents {
		record := state.Accounts[label]
		if record == nil || consent.ClientID != record.ClientID || consent.Subject == "" || consent.Subject != record.Subject {
			return Error("Invalid persisted SIWC consent identity")
		}
	}
	return nil
}

func (s *Store) read() (*State, error) {
	data, err := s.memory, error(nil)
	if data == nil {
		data, err = privateRead(filepath.Join(s.Root, "state.json"), maxState)
	}
	if errors.Is(err, os.ErrNotExist) {
		return &State{Version: 1, HostID: uuid.New().URN(), Accounts: map[string]*Record{}, Routes: map[string]Route{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var state State
	if json.Unmarshal(data, &state) != nil {
		return nil, Error("Malformed SIWC state; restore protected state or re-enroll")
	}
	if err := state.validate(); err != nil {
		return nil, err
	}
	return &state, nil
}

func (s *Store) write(state *State) error {
	if err := state.validate(); err != nil {
		return err
	}
	if s.memory != nil {
		data, err := json.Marshal(state)
		if err == nil {
			s.memory = data
		}
		return err
	}
	target := filepath.Join(s.Root, "state.json")
	if _, err := privateRead(target, maxState); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// A dangling symlink is not an absent state file.
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return Error("Unsafe SIWC state file")
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(s.Root, ".siwc-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(file.Name(), target); err != nil {
		return err
	}
	parent, err := os.Open(s.Root)
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}

func clearTokens(record *Record) {
	record.AccessToken = ""
	record.RefreshToken = ""
	record.IDToken = ""
	record.ExpiresAt = 0
	record.EarliestRefreshAt = 0
	record.RefreshPending = false
}
