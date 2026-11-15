package controllerauthority

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

var (
	ErrHostExclusionBusy = errors.New("host controller enrollment exclusion is busy")
	ErrHostExclusionInvalid = errors.New("host controller enrollment exclusion is invalid")
)

// HostEnrollmentExclusion serializes cooperating host-administrator operations
// with the SAME enrollment lock used by root Create/Revoke. It is not an Incus
// authorization token, a cross-UID broker, a distributed lease or a fence for
// manual/noncooperating Incus writers. Never pass this to an untrusted user.
type HostEnrollmentExclusion struct {
	mu sync.Mutex
	lock *os.File
	directory string
	owner uint32
	selection SelectedRevision
	action string
	epoch uint64
	controllerID string
	closed bool
}

// AcquireHostEnrollmentExclusion admits ONLY the root host controller through
// the fixed, trusted administrator directory. An operator-owned enrollment
// cannot be acquired using this root-only API. The future Home Manager
// controller requires a separate authenticated protocol and independent TLS
// authority; it must never borrow this exclusion as a privileged proxy.
func AcquireHostEnrollmentExclusion(
	ctx context.Context, selection SelectedRevision, action string,
) (*HostEnrollmentExclusion, error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("host controller exclusion requires root")
	}
	return acquireHostExclusionAt(ctx, hostEnrollmentDir, 0, selection, action)
}

// acquireHostExclusionAt is private and accepts an alternate owner/path only
// for hermetic same-UID test fixtures. The production API above is fixed.
func acquireHostExclusionAt(
	ctx context.Context, dir string, owner uint32, selection SelectedRevision, action string,
) (*HostEnrollmentExclusion, error) {
	if ctx == nil {
		return nil, ErrHostExclusionInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(dir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open host controller exclusion directory: %w", err)
	}
	defer syscall.Close(fd)
	if err := validateOwnerFD(fd, owner, true); err != nil {
		return nil, err
	}
	lockFD, err := syscall.Openat(fd, hostEnrollmentLock,
		syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open existing host controller exclusion: %w", err)
	}
	lock := os.NewFile(uintptr(lockFD), "host controller exclusion")
	fail := func(err error) (*HostEnrollmentExclusion, error) {
		_ = lock.Close()
		return nil, err
	}
	if err := validateOwnerFD(lockFD, owner, false); err != nil {
		return fail(err)
	}
	if err := syscall.Flock(lockFD, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return fail(ErrHostExclusionBusy)
		}
		return fail(errors.New("cannot acquire host controller exclusion"))
	}
	opened, err := lock.Stat()
	if err != nil {
		return fail(err)
	}
	named, err := os.Lstat(filepath.Join(dir, hostEnrollmentLock))
	if err != nil || !os.SameFile(opened, named) {
		return fail(ErrHostExclusionInvalid)
	}
	guard := &HostEnrollmentExclusion{
		lock: lock, directory: dir, owner: owner,
		selection: selection, action: action,
	}
	if err := guard.recheck(ctx, false); err != nil {
		return fail(err)
	}
	return guard, nil
}

// Check re-reads the *current* host policy and compares both the selection
// and the original controller/epoch while the cooperative exclusion is held.
// It MUST be invoked again at a future operation's mutation boundary, and
// even success never substitutes for accepted-state, TLS, Incus resource
// policy, ETag or independent external-writer authorization.
func (g *HostEnrollmentExclusion) Check(ctx context.Context) error {
	if g == nil {
		return ErrHostExclusionInvalid
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return ErrHostExclusionInvalid
	}
	return g.recheck(ctx, true)
}

func (g *HostEnrollmentExclusion) recheck(ctx context.Context, requireSame bool) error {
	if ctx == nil {
		return ErrHostExclusionInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	observed, err := loadOwnerFile(g.directory, g.owner)
	if err != nil {
		return fmt.Errorf("recheck host enrollment policy: %w", err)
	}
	assessment := observed.Assess(AssessedPrincipal{UID: 0}, g.selection, g.action)
	if assessment.Code != EnrollmentMatched || assessment.Epoch == 0 ||
		assessment.ControllerID == "" {
		return ErrHostExclusionInvalid
	}
	// A host-held exclusion cannot claim an operator identity as root; the
	// type's Assess check rejects operator UID/certificate automatically.
	if requireSame && (assessment.Epoch != g.epoch || assessment.ControllerID != g.controllerID) {
		return ErrHostExclusionInvalid
	}
	if !requireSame {
		g.epoch = assessment.Epoch
		g.controllerID = assessment.ControllerID
	}
	return ctx.Err()
}

// Close releases the cooperative enrollment lock. No operation may rely on
// Check after Close, and OS process death releases this flock automatically.
func (g *HostEnrollmentExclusion) Close() error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil
	}
	g.closed = true
	return g.lock.Close()
}
