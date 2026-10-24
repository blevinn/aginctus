package testenv

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"
)

// BackendName identifies the provider of an outer disposable test VM.
// Batch NixOS test-driver scenarios remain independent of this API.
type BackendName string

const IncusBackend BackendName = "incus"

var (
	ErrUnsupportedBackend = errors.New("unsupported testenv VM backend")
	ErrBackendUnavailable = errors.New("testenv VM backend is not installed")
	ErrUnsupportedAction  = errors.New("testenv VM operation is unsupported")
	ErrOwnershipMismatch  = errors.New("testenv VM ownership mismatch")
)

const (
	DefaultVMProject = "aginctus-testenv"
	MaxVMTimeToLive  = 2 * time.Hour
	MaxGuestOutput   = 64 * 1024
)

var (
	vmSafeName = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)
	vmDigest   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	vmRunID    = regexp.MustCompile(`^vm-[0-9a-f]{24}$`)
)

// BackendCapabilities are implementation-specific optional operations.
type BackendCapabilities struct {
	Exec     bool `json:"exec"`
	Snapshot bool `json:"snapshot"`
	Reset    bool `json:"reset"`
}

// BackendDescriptor is offline discovery, not proof that an Incus server
// is reachable or an operation is currently authorized.
type BackendDescriptor struct {
	Name            BackendName `json:"name"`
	Lifecycle       string      `json:"lifecycle"`
	Project         string      `json:"project"`
	ImagePolicy     string      `json:"image_policy"`
	NetworkPolicy   string      `json:"network_policy"`
	AgentAccessible bool        `json:"agent_accessible"`
}

func SupportedVMBackends() []BackendDescriptor {
	return []BackendDescriptor{{
		Name:            IncusBackend,
		Lifecycle:       "planned",
		Project:         DefaultVMProject,
		ImagePolicy:     "pinned sha256 fingerprint required",
		NetworkPolicy:   "no outer NIC by default",
		AgentAccessible: false,
	}}
}

func VMBackendDescriptor(name string) (BackendDescriptor, error) {
	for _, item := range SupportedVMBackends() {
		if name == string(item.Name) {
			return item, nil
		}
	}
	return BackendDescriptor{}, fmt.Errorf("%w: %q", ErrUnsupportedBackend, name)
}

// VMCreateSpec is assembled by the trusted control service. It is never
// exposed as a free-form provider config to an MCP caller.
// VMRetention is chosen only by trusted worker policy. An empty value
// defaults to disposable Incus ephemeral behavior. Retained instances are
// explicit and still subject to TTL-based destruction.
type VMRetention string

const (
	VMEphemeral VMRetention = "ephemeral"
	VMRetained  VMRetention = "retained"
)

type VMCreateSpec struct {
	Backend          BackendName
	Project          string
	ImageFingerprint string
	CPU              int
	MemoryMiB        int
	RootDiskGiB      int
	TimeToLive       time.Duration
	Retention        VMRetention
}

func (spec VMCreateSpec) Validate() error {
	if _, err := VMBackendDescriptor(string(spec.Backend)); err != nil {
		return err
	}
	if spec.Retention != "" && spec.Retention != VMEphemeral && spec.Retention != VMRetained {
		return errors.New("unsupported testenv VM retention policy")
	}
	if spec.Project != DefaultVMProject {
		return errors.New("testenv VM must use the dedicated restricted project")
	}
	if !vmDigest.MatchString(spec.ImageFingerprint) {
		return errors.New("testenv VM requires a lowercase 64-character image fingerprint")
	}
	if spec.CPU < 1 || spec.CPU > 2 ||
		spec.MemoryMiB < 512 || spec.MemoryMiB > 5120 ||
		spec.RootDiskGiB < 8 || spec.RootDiskGiB > 32 {
		return errors.New("testenv VM resource limits exceed admitted range")
	}
	if spec.TimeToLive < time.Minute || spec.TimeToLive > MaxVMTimeToLive {
		return errors.New("testenv VM lifetime outside admitted bounds")
	}
	return nil
}

// VMRef is a scoped opaque handle, not an authorization token. The backend
// MUST authenticate actual Incus ownership/project/generation before change.
type VMRef struct {
	Backend          BackendName `json:"backend"`
	Project          string      `json:"project"`
	Instance         string      `json:"instance"`
	RunID            string      `json:"run_id"`
	ImageFingerprint string      `json:"image_fingerprint"`
	Policy           VMRunPolicy `json:"policy"`
	PolicyMAC        string      `json:"policy_mac"`
}

func (ref VMRef) Validate() error {
	if _, err := VMBackendDescriptor(string(ref.Backend)); err != nil {
		return err
	}
	if ref.Project != DefaultVMProject ||
		!vmSafeName.MatchString(ref.Instance) ||
		!vmRunID.MatchString(ref.RunID) ||
		!vmDigest.MatchString(ref.ImageFingerprint) {
		return ErrOwnershipMismatch
	}
	return nil
}

type VMStatus struct {
	Ref       VMRef     `json:"ref"`
	State     string    `json:"state"`
	ExpiresAt time.Time `json:"expires_at"`
}

type VMExecResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// VMBackend is implemented only in the trusted worker. Every mutation
// must reverify ownership and enforce bounded argv, time and output.
type VMBackend interface {
	Name() BackendName
	Capabilities() BackendCapabilities
	Create(context.Context, VMCreateSpec) (VMRef, error)
	Status(context.Context, VMRef) (VMStatus, error)
	Exec(context.Context, VMRef, []string, time.Duration) (VMExecResult, error)
	Snapshot(context.Context, VMRef, string) error
	Reset(context.Context, VMRef, string) error
	Destroy(context.Context, VMRef) error
	ListOwned(context.Context) ([]VMStatus, error)
}

// BackendRegistry accepts only explicit, trusted worker-injected backends.
type BackendRegistry struct {
	installed map[BackendName]VMBackend
}

func NewBackendRegistry(backends ...VMBackend) (*BackendRegistry, error) {
	reg := &BackendRegistry{installed: make(map[BackendName]VMBackend)}
	for _, backend := range backends {
		if backend == nil {
			return nil, errors.New("nil VM backend")
		}
		if _, err := VMBackendDescriptor(string(backend.Name())); err != nil {
			return nil, err
		}
		if _, exists := reg.installed[backend.Name()]; exists {
			return nil, errors.New("duplicate VM backend")
		}
		reg.installed[backend.Name()] = backend
	}
	return reg, nil
}

func (r *BackendRegistry) Resolve(name string) (VMBackend, error) {
	desc, err := VMBackendDescriptor(name)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrBackendUnavailable
	}
	backend, ok := r.installed[desc.Name]
	if !ok {
		return nil, ErrBackendUnavailable
	}
	return backend, nil
}
