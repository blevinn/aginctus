package firewall

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// EgressStateFile journals pending intent before non-atomic firewalld setters.
type EgressStateFile string

func (path EgressStateFile) Load() (EgressState, error) {
	var state EgressState
	raw, err := os.ReadFile(string(path))
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(raw, &state)
	return state, err
}

func (path EgressStateFile) Save(state EgressState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(string(path)), ".egress-state-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), string(path))
}

// LockEgress serializes Aginctus lifecycle operations; root maintenance must
// coordinate separately because the lock cannot serialize other authorities.
func LockEgress(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	fd, err := unix.Open(filepath.Join(dir, "lock"), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("another Aginctus firewall operation holds the lifecycle lock: %w", err)
	}
	return func() { unix.Close(fd) }, nil
}
