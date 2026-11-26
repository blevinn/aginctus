package testenv

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/blevinn/aginctus/internal/filestate"
)

const maxRecoveryLedgerBytes = 128 * 1024

// recoveryLedger retains signed references across an uncertain cleanup.
// Entries authorize no new mutation; each sweep still uses live discovery
// and ownership checks. The installed worker owns this private directory.
type recoveryLedger struct {
	SchemaVersion int     `json:"schema_version"`
	Pending       []VMRef `json:"pending"`
	path          string
}

func openRecoveryLedger(dir string, b *IncusVMBackend, now time.Time) (*recoveryLedger, *os.File, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return nil, nil, errors.New("invalid recovery ledger directory")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return nil, nil, errors.New("recovery ledger requires a private directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return nil, nil, errors.New("recovery ledger directory ownership unverified")
	}
	path := filepath.Join(dir, "pending.json")
	lock, err := filestate.LockRegistry(path)
	if err != nil {
		return nil, nil, errors.New("recovery ledger lock unavailable")
	}
	ledger := &recoveryLedger{SchemaVersion: 1, path: path}
	data, err := filestate.ReadPrivateFile(path, maxRecoveryLedgerBytes)
	if errors.Is(err, os.ErrNotExist) {
		return ledger, lock, nil
	}
	if err != nil || json.Unmarshal(data, ledger) != nil || ledger.SchemaVersion != 1 || len(ledger.Pending) > MaxIncusReconcileCandidates {
		lock.Close()
		return nil, nil, errors.New("invalid recovery ledger")
	}
	seen := make(map[string]bool)
	for _, ref := range ledger.Pending {
		if b.verifyRef(ref) != nil || ref.Instance != "testenv-"+ref.RunID || now.Before(ref.Policy.ExpiresAt) || seen[ref.Instance] {
			lock.Close()
			return nil, nil, ErrOwnershipMismatch
		}
		seen[ref.Instance] = true
	}
	return ledger, lock, nil
}

func (l *recoveryLedger) save() error {
	data, err := json.Marshal(l)
	if err != nil || len(data) >= maxRecoveryLedgerBytes {
		return errors.New("recovery ledger exceeds bound")
	}
	if err := filestate.WritePrivateFile(l.path, append(data, '\n')); err != nil {
		return errors.New("recovery ledger persistence failed")
	}
	dir, err := os.Open(filepath.Dir(l.path))
	if err != nil {
		return errors.New("recovery ledger durability unverified")
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return errors.New("recovery ledger durability unverified")
	}
	return nil
}

// Record all eligible handles before any mutation. Do not replace a pending
// reference with a new generation, even if an external writer reused a name.
func (l *recoveryLedger) record(owned []VMStatus, now time.Time) error {
	seen := make(map[string]VMRef)
	for _, ref := range l.Pending {
		seen[ref.Instance] = ref
	}
	for _, vm := range owned {
		if ref, ok := seen[vm.Ref.Instance]; ok {
			if ref.PolicyMAC != vm.Ref.PolicyMAC {
				return ErrOwnershipMismatch
			}
			continue
		}
		if now.Before(vm.ExpiresAt) {
			continue
		}
		if len(l.Pending) >= MaxIncusReconcileCandidates {
			return errors.New("recovery ledger capacity exceeded")
		}
		l.Pending = append(l.Pending, vm.Ref)
		seen[vm.Ref.Instance] = vm.Ref
	}
	return l.save()
}

func (l *recoveryLedger) resolved(name string) {
	for i, ref := range l.Pending {
		if ref.Instance == name {
			l.Pending = append(l.Pending[:i], l.Pending[i+1:]...)
			return
		}
	}
}
