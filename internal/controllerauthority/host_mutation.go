package controllerauthority

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const hostEnrollmentLock = "controller-enrollment.json.flock"

// EnrollmentReference names an exact previously enrolled resource and owner.
// No implicit "all", ambient project, or owner fallback is permitted.
type EnrollmentReference struct {
	ControllerID string
	DeploymentID string
	IncusProject string
	WorkloadID   string
}

// CreateHostControllerEnrollment is an explicit root-only ADMINISTRATIVE
// operation. No Home Manager user, API caller, or state-service peer can
// invoke this with another policy path. It does NOT grant Incus permissions,
// quiesce old writers, or issue a live fence; it only records host intent.
func CreateHostControllerEnrollment(ctx context.Context, requested Enrollment) (Enrollment, error) {
	if os.Geteuid() != 0 {
		return Enrollment{}, errors.New("host controller enrollment requires root")
	}
	return createAt(ctx, hostEnrollmentDir, 0, requested)
}

// RevokeHostControllerEnrollment retains a durable revoked tombstone with an
// incremented epoch. Revocation of this file does not stop an in-flight Incus
// operation; a future executor must independently honor host revocation.
func RevokeHostControllerEnrollment(ctx context.Context, ref EnrollmentReference, expectedEpoch uint64) (Enrollment, error) {
	if os.Geteuid() != 0 {
		return Enrollment{}, errors.New("host controller revocation requires root")
	}
	return revokeAt(ctx, hostEnrollmentDir, 0, ref, expectedEpoch)
}

func createAt(ctx context.Context, dir string, owner uint32, requested Enrollment) (Enrollment, error) {
	if ctx == nil {
		return Enrollment{}, errors.New("host enrollment requires a context")
	}
	if requested.Status != "active" || requested.Epoch != 1 {
		return Enrollment{}, errors.New("new enrollment requires active status and initial epoch one")
	}
	if err := requested.validate(); err != nil {
		return Enrollment{}, err
	}
	return mutateAt(ctx, dir, owner, func(existing []Enrollment) ([]Enrollment, Enrollment, error) {
		for _, record := range existing {
			if resourceKey(record) == resourceKey(requested) {
				// Durable tombstones prevent implicit adoption/re-enrollment;
				// a later explicit handoff protocol will be required.
				return nil, Enrollment{}, errors.New("controller enrollment exists or was previously revoked")
			}
		}
		return append(existing, requested), requested, nil
	})
}

func revokeAt(ctx context.Context, dir string, owner uint32, ref EnrollmentReference, expectedEpoch uint64) (Enrollment, error) {
	if ctx == nil || expectedEpoch == 0 ||
		!idPattern.MatchString(ref.ControllerID) ||
		!idPattern.MatchString(ref.DeploymentID) ||
		!idPattern.MatchString(ref.IncusProject) || ref.IncusProject == "default" ||
		!idPattern.MatchString(ref.WorkloadID) {
		return Enrollment{}, errors.New("revocation requires exact controller, scope and nonzero expected epoch")
	}
	return mutateAt(ctx, dir, owner, func(existing []Enrollment) ([]Enrollment, Enrollment, error) {
		for i := range existing {
			e := existing[i]
			if e.DeploymentID != ref.DeploymentID || e.IncusProject != ref.IncusProject ||
				e.WorkloadID != ref.WorkloadID {
				continue
			}
			if e.ControllerID != ref.ControllerID || e.Epoch != expectedEpoch ||
				e.Status != "active" || e.Epoch == ^uint64(0) {
				return nil, Enrollment{}, errors.New("controller revocation CAS conflict")
			}
			e.Epoch++
			e.Status = "revoked"
			existing[i] = e
			return existing, e, nil
		}
		return nil, Enrollment{}, errors.New("controller to revoke was not enrolled")
	})
}

// mutateAt is private so only the hardcoded root-owned public endpoints may
// write production state. Its owner argument is for same-UID hermetic tests.
// The persistent flock covers participating administrative writers; the file
// is not a live controller lease that can exclude outside Incus clients.
func mutateAt(ctx context.Context, dir string, owner uint32, edit func([]Enrollment) ([]Enrollment, Enrollment, error)) (Enrollment, error) {
	if err := ctx.Err(); err != nil {
		return Enrollment{}, err
	}
	fd, err := syscall.Open(dir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return Enrollment{}, fmt.Errorf("open host authority store: %w", err)
	}
	defer syscall.Close(fd)
	if err := validateOwnerFD(fd, owner, true); err != nil {
		return Enrollment{}, err
	}
	lockFD, err := syscall.Openat(fd, hostEnrollmentLock,
		syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return Enrollment{}, fmt.Errorf("open host authority writer lock: %w", err)
	}
	lock := os.NewFile(uintptr(lockFD), "host enrollment writer lock")
	defer lock.Close()
	if err := validateOwnerFD(lockFD, owner, false); err != nil {
		return Enrollment{}, err
	}
	if err := syscall.Flock(lockFD, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return Enrollment{}, errors.New("host enrollment writer lock is busy or unavailable")
	}
	// Never unlink a lock inode; a replaced path would split cooperative
	// writers even if each process successfully acquired its own file lock.
	opened, err := lock.Stat()
	if err != nil {
		return Enrollment{}, err
	}
	named, err := os.Lstat(filepath.Join(dir, hostEnrollmentLock))
	if err != nil || !os.SameFile(opened, named) {
		return Enrollment{}, errors.New("host enrollment lock identity changed")
	}

	var existing []Enrollment
	evidence, err := loadOwnerFile(dir, owner)
	if errors.Is(err, os.ErrNotExist) {
		// Missing file is a permitted genesis state ONLY on explicit
		// administrator creation, never on a read-only caller path.
		existing = []Enrollment{}
	} else if err != nil {
		return Enrollment{}, err
	} else {
		existing = evidence.entries
	}
	next, result, err := edit(existing)
	if err != nil {
		return Enrollment{}, err
	}
	raw, err := json.Marshal(enrollmentFile{APIVersion: EnrollmentVersion, Enrollments: next})
	if err != nil || len(raw) > maxEnrollmentBytes {
		return Enrollment{}, errors.New("host enrollment would exceed size bound")
	}
	if _, err := parse(raw); err != nil {
		return Enrollment{}, err
	}
	if err := ctx.Err(); err != nil {
		return Enrollment{}, err
	}
	tmp, err := os.CreateTemp(dir, ".controller-enrollment-*")
	if err != nil {
		return Enrollment{}, err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return Enrollment{}, err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return Enrollment{}, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return Enrollment{}, err
	}
	if err := tmp.Close(); err != nil {
		return Enrollment{}, err
	}
	if err := ctx.Err(); err != nil {
		return Enrollment{}, err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, hostEnrollmentFile)); err != nil {
		return Enrollment{}, err
	}
	// An error after rename is potentially an uncertain durable outcome.
	// Never report it as a definitely failed change; reread host policy.
	if err := syscall.Fsync(fd); err != nil {
		return result, fmt.Errorf("host enrollment publication may be durable; reread before retry: %w", err)
	}
	return result, nil
}
