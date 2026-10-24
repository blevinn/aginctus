package testenv

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrIncusInstanceMissing is an adapter error for a missing instance within
// the dedicated project, NOT authorization to destroy a foreign instance.
var ErrIncusInstanceMissing = errors.New("testenv Incus instance not found")

// IncusVMInstance is the subset of Incus state the security checks need.
// No caller-provided raw maps are sent to Incus without a fixed policy plan.
type IncusVMInstance struct {
	Name      string
	Type      string
	Status    string
	Ephemeral bool
	Profiles  []string
	Config    map[string]string
	Devices   map[string]map[string]string
}

// IncusVMClient is project-bound, supplied only by an installed trusted
// worker. The testenv CLI and MCP cannot instantiate this or choose a socket.
type IncusVMClient interface {
	ProjectConfig(context.Context) (map[string]string, error)
	HasImage(context.Context, string) (bool, error)
	Get(context.Context, string) (*IncusVMInstance, error)
	Create(context.Context, IncusInstancePlan) error
	Start(context.Context, string) error
	Stop(context.Context, string) error
	Delete(context.Context, string) error
	List(context.Context) ([]IncusVMInstance, error)
}

// IncusVMBackend is a deliberately minimal native-lifecycle implementation.
// Guest exec and snapshots/reset are not admitted in this slice.
type IncusVMBackend struct {
	client    IncusVMClient
	pool      string
	now       func() time.Time
	policyKey []byte
}

var _ VMBackend = (*IncusVMBackend)(nil)

func NewIncusVMBackend(client IncusVMClient, storagePool string, policyKey []byte) (*IncusVMBackend, error) {
	if client == nil {
		return nil, errors.New("trusted Incus client required")
	}
	if _, err := IncusProjectPolicy(storagePool); err != nil {
		return nil, err
	}
	if len(policyKey) != 32 {
		return nil, errors.New("a stable trusted 32-byte worker policy key is required")
	}
	return &IncusVMBackend{client: client, pool: storagePool, now: time.Now, policyKey: append([]byte(nil), policyKey...)}, nil
}

func (*IncusVMBackend) Name() BackendName                 { return IncusBackend }
func (*IncusVMBackend) Capabilities() BackendCapabilities { return BackendCapabilities{} }

// checkProject refuses a mutable/default/looser project. The worker must
// provision a dedicated project first; no implicit creation or adoption.
func (b *IncusVMBackend) checkProject(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	want, err := IncusProjectPolicy(b.pool)
	if err != nil {
		return err
	}
	actual, err := b.client.ProjectConfig(ctx)
	if err != nil {
		return safeIncusError("testenv Incus project policy unavailable", err)
	}
	for key, expected := range want.Config {
		if actual[key] != expected {
			return ErrOwnershipMismatch
		}
	}
	// Additional provider-defined project controls are allowed only when
	// they do not introduce an unreviewed restricted/limit override.
	for key := range actual {
		if strings.HasPrefix(key, "restricted.") || strings.HasPrefix(key, "limits.") {
			if _, ok := want.Config[key]; !ok {
				return ErrOwnershipMismatch
			}
		}
	}
	return nil
}

func newVMRunID() (string, error) {
	var token [12]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", errors.New("VM identity generation failed")
	}
	return "vm-" + hex.EncodeToString(token[:]), nil
}

func (b *IncusVMBackend) checkIncusOwned(actual *IncusVMInstance, ref VMRef) (time.Time, error) {
	if err := b.verifyRef(ref); err != nil {
		return time.Time{}, err
	}
	if actual == nil || actual.Name != ref.Instance ||
		actual.Type != "virtual-machine" ||
		actual.Name != "testenv-"+ref.RunID ||
		len(actual.Profiles) != 0 ||
		actual.Config["user.aginctus.testenv.run"] != ref.RunID ||
		actual.Config["user.aginctus.testenv.image"] != ref.ImageFingerprint {
		return time.Time{}, ErrOwnershipMismatch
	}
	// A root disk may be managed by Incus, never a host bind source.
	root, ok := actual.Devices["root"]
	if !ok || len(actual.Devices) != 1 ||
		root["type"] != "disk" || root["path"] != "/" ||
		root["source"] != "" || root["pool"] != b.pool ||
		root["size"] != fmt.Sprintf("%dGiB", ref.Policy.RootDiskGiB) ||
		actual.Config["limits.cpu"] != strconv.Itoa(ref.Policy.CPU) ||
		actual.Config["limits.memory"] != fmt.Sprintf("%dMiB", ref.Policy.MemoryMiB) ||
		actual.Config["security.nesting"] != "true" ||
		actual.Ephemeral != (ref.Policy.Retention == VMEphemeral) {
		return time.Time{}, ErrOwnershipMismatch
	}
	expiry, err := time.Parse(time.RFC3339Nano, actual.Config["user.aginctus.testenv.expires"])
	if err != nil || expiry.IsZero() {
		return time.Time{}, ErrOwnershipMismatch
	}
	stored, err := b.instanceRef(*actual)
	if err != nil || stored != ref || !expiry.Equal(ref.Policy.ExpiresAt) {
		return time.Time{}, ErrOwnershipMismatch
	}
	return expiry, nil
}

func (b *IncusVMBackend) Create(ctx context.Context, spec VMCreateSpec) (VMRef, error) {
	if b == nil || b.client == nil {
		return VMRef{}, ErrBackendUnavailable
	}
	if err := spec.Validate(); err != nil {
		return VMRef{}, err
	}
	if err := ctx.Err(); err != nil {
		return VMRef{}, err
	}
	if err := b.checkProject(ctx); err != nil {
		return VMRef{}, err
	}
	exists, err := b.client.HasImage(ctx, spec.ImageFingerprint)
	if err != nil || !exists {
		return VMRef{}, safeIncusError("pinned VM image unavailable from trusted project", err)
	}
	runID, err := newVMRunID()
	if err != nil {
		return VMRef{}, err
	}
	ref := VMRef{
		Backend: IncusBackend, Project: spec.Project,
		Instance: "testenv-" + runID, RunID: runID,
		ImageFingerprint: spec.ImageFingerprint,
	}
	created := b.now().UTC()
	retention := spec.Retention
	if retention == "" {
		retention = VMEphemeral
	}
	ref.Policy = VMRunPolicy{CPU: spec.CPU, MemoryMiB: spec.MemoryMiB, RootDiskGiB: spec.RootDiskGiB,
		Retention: retention, StoragePool: b.pool, CreatedAt: created, ExpiresAt: created.Add(spec.TimeToLive)}
	ref.PolicyMAC = b.sealRef(ref)
	plan, err := PlanIncusVM(spec, ref, b.pool, created)
	if err != nil {
		return VMRef{}, err
	}
	policy, err := json.Marshal(ref.Policy)
	if err != nil {
		return VMRef{}, errors.New("cannot encode trusted run policy")
	}
	plan.Config[incusPolicyKey] = string(policy)
	plan.Config[incusPolicyMACKey] = ref.PolicyMAC
	// Fail closed on collisions, including an existing instance with an
	// apparently matching user.* owner tag. Never adopt an existing name.
	if _, err := b.client.Get(ctx, ref.Instance); err == nil {
		return VMRef{}, errors.New("testenv VM name already exists")
	} else if !errors.Is(err, ErrIncusInstanceMissing) {
		return VMRef{}, safeIncusError("cannot verify testenv VM name is unused", err)
	}
	if err := b.client.Create(ctx, plan); err != nil {
		// Submission may have succeeded even when its response/wait failed.
		// The sealed handle permits verification, never automatic adoption.
		return ref, safeIncusError("testenv VM create outcome uncertain; recovery required", err)
	}
	actual, err := b.client.Get(ctx, ref.Instance)
	if err != nil {
		return ref, safeIncusError("created VM requires recovery: cannot verify instance", err)
	}
	if _, err := b.checkIncusOwned(actual, ref); err != nil || actual.Ephemeral != plan.Ephemeral ||
		actual.Devices["root"]["pool"] != b.pool {
		return ref, ErrOwnershipMismatch
	}
	// An instance can exist after a crash between Create and Start. The
	// trusted orphan/TTL reconciler will handle that; never blindly retry.
	if err := b.client.Start(ctx, ref.Instance); err != nil {
		return ref, safeIncusError("created VM requires recovery: start failed", err)
	}
	return ref, nil
}

func (b *IncusVMBackend) loadOwned(ctx context.Context, ref VMRef) (*IncusVMInstance, time.Time, error) {
	if b == nil || b.client == nil {
		return nil, time.Time{}, ErrBackendUnavailable
	}
	if err := b.verifyRef(ref); err != nil {
		return nil, time.Time{}, err
	}
	if err := b.checkProject(ctx); err != nil {
		return nil, time.Time{}, err
	}
	actual, err := b.client.Get(ctx, ref.Instance)
	if err != nil {
		if actual != nil {
			return nil, time.Time{}, ErrOwnershipMismatch
		}
		return nil, time.Time{}, safeIncusError("testenv VM lookup failed", err)
	}
	expiry, err := b.checkIncusOwned(actual, ref)
	if err != nil || actual.Devices["root"]["pool"] != b.pool {
		return nil, time.Time{}, ErrOwnershipMismatch
	}
	return actual, expiry, nil
}

func (b *IncusVMBackend) Status(ctx context.Context, ref VMRef) (VMStatus, error) {
	actual, expiry, err := b.loadOwned(ctx, ref)
	if err != nil {
		return VMStatus{}, err
	}
	return VMStatus{Ref: ref, State: actual.Status, ExpiresAt: expiry}, nil
}

func (b *IncusVMBackend) Destroy(ctx context.Context, ref VMRef) error {
	actual, _, err := b.loadOwned(ctx, ref)
	if errors.Is(err, ErrIncusInstanceMissing) {
		return b.confirmAbsent(ctx, ref)
	}
	if err != nil {
		return err
	}
	// A normal Stop deletes an ephemeral instance. Retained VMs require
	// a distinct explicit Delete. Never turn missing-after-stop into
	// permission to affect any different instance.
	if !strings.EqualFold(actual.Status, "Running") &&
		!strings.EqualFold(actual.Status, "Stopped") {
		return errors.New("VM in unexpected state; cleanup requires reconciliation")
	}
	if strings.EqualFold(actual.Status, "Running") {
		if err := b.client.Stop(ctx, ref.Instance); err != nil {
			if errors.Is(err, ErrIncusInstanceMissing) {
				return b.confirmAbsent(ctx, ref)
			}
			return safeIncusError("testenv VM stop failed; cleanup unverified", err)
		}
		actual, _, err = b.loadOwned(ctx, ref)
		if errors.Is(err, ErrIncusInstanceMissing) {
			if actual != nil {
				return errors.New("ambiguous VM cleanup response")
			}
			return b.confirmAbsent(ctx, ref)
		}
		if err != nil {
			return safeIncusError("testenv VM cleanup recheck failed", err)
		}
		if !strings.EqualFold(actual.Status, "Stopped") {
			return errors.New("testenv VM did not stop; cleanup unverified")
		}
	}
	if err := b.client.Delete(ctx, ref.Instance); err != nil {
		if !errors.Is(err, ErrIncusInstanceMissing) {
			return safeIncusError("testenv VM deletion not verified", err)
		}
	}
	return b.confirmAbsent(ctx, ref)
}

// Absence verifies only a project-scoped postcondition, not former ownership
// or separate volume/evidence reclamation. A policy failure is never absence.
func (b *IncusVMBackend) confirmAbsent(ctx context.Context, ref VMRef) error {
	if err := b.verifyRef(ref); err != nil {
		return err
	}
	if err := b.checkProject(ctx); err != nil {
		return err
	}
	actual, err := b.client.Get(ctx, ref.Instance)
	if actual == nil && errors.Is(err, ErrIncusInstanceMissing) && ctx.Err() == nil {
		return nil
	}
	return safeIncusError("testenv VM still present or unverifiable after deletion", err)
}

func (b *IncusVMBackend) ListOwned(ctx context.Context) ([]VMStatus, error) {
	if b == nil || b.client == nil {
		return nil, ErrBackendUnavailable
	}
	if err := b.checkProject(ctx); err != nil {
		return nil, err
	}
	all, err := b.client.List(ctx)
	if err != nil {
		return nil, safeIncusError("unable to enumerate testenv Incus project", err)
	}
	owned := make([]VMStatus, 0, len(all))
	for _, instance := range all {
		runID := instance.Config["user.aginctus.testenv.run"]
		if !vmRunID.MatchString(runID) || instance.Name != "testenv-"+runID {
			continue // unrelated project resource; do not adopt it
		}
		ref, err := b.instanceRef(instance)
		if err != nil {
			return nil, ErrOwnershipMismatch
		}
		expiry, err := b.checkIncusOwned(&instance, ref)
		if err != nil || instance.Devices["root"]["pool"] != b.pool {
			return nil, ErrOwnershipMismatch // suspicious owned prefix
		}
		owned = append(owned, VMStatus{Ref: ref, State: instance.Status, ExpiresAt: expiry})
	}
	return owned, nil
}

// These remain deliberately unavailable until Incus agent exec and
// restricted snapshot handling have their own negative-security tests.
func (*IncusVMBackend) Exec(context.Context, VMRef, []string, time.Duration) (VMExecResult, error) {
	return VMExecResult{}, ErrUnsupportedAction
}
func (*IncusVMBackend) Snapshot(context.Context, VMRef, string) error { return ErrUnsupportedAction }
func (*IncusVMBackend) Reset(context.Context, VMRef, string) error    { return ErrUnsupportedAction }
