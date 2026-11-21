package testenv

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// VMRunPolicy is authenticated by the worker, not reconstructed from mutable
// resource limits. The worker must retain its signing key across restarts.
type VMRunPolicy struct {
	CPU         int         `json:"cpu"`
	MemoryMiB   int         `json:"memory_mib"`
	RootDiskGiB int         `json:"root_disk_gib"`
	Retention   VMRetention `json:"retention"`
	StoragePool string      `json:"storage_pool"`
	CreatedAt   time.Time   `json:"created_at"`
	ExpiresAt   time.Time   `json:"expires_at"`
}

const (
	incusPolicyKey    = "user.aginctus.testenv.policy"
	incusPolicyMACKey = "user.aginctus.testenv.policy-mac"
)

func (b *IncusVMBackend) sealRef(ref VMRef) string {
	ref.PolicyMAC = ""
	data, _ := json.Marshal(ref) // fixed, JSON-serializable fields only
	mac := hmac.New(sha256.New, b.policyKey)
	_, _ = mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

func (b *IncusVMBackend) verifyRef(ref VMRef) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	p := ref.Policy
	spec := VMCreateSpec{
		Backend: ref.Backend, Project: ref.Project, ImageFingerprint: ref.ImageFingerprint,
		CPU: p.CPU, MemoryMiB: p.MemoryMiB, RootDiskGiB: p.RootDiskGiB,
		Retention: p.Retention, TimeToLive: p.ExpiresAt.Sub(p.CreatedAt),
	}
	if p.CreatedAt.IsZero() || p.StoragePool != b.pool || spec.Validate() != nil ||
		(p.Retention != VMEphemeral && p.Retention != VMRetained) {
		return ErrOwnershipMismatch
	}
	provided, err := hex.DecodeString(ref.PolicyMAC)
	want, _ := hex.DecodeString(b.sealRef(ref))
	if err != nil || !hmac.Equal(provided, want) {
		return ErrOwnershipMismatch
	}
	return nil
}

func (b *IncusVMBackend) instanceRef(instance IncusVMInstance) (VMRef, error) {
	ref := VMRef{
		Backend: IncusBackend, Project: DefaultVMProject, Instance: instance.Name,
		RunID:            instance.Config["user.aginctus.testenv.run"],
		ImageFingerprint: instance.Config["user.aginctus.testenv.image"],
		PolicyMAC:        instance.Config[incusPolicyMACKey],
	}
	if len(instance.Config[incusPolicyKey]) > 4096 ||
		json.Unmarshal([]byte(instance.Config[incusPolicyKey]), &ref.Policy) != nil {
		return VMRef{}, ErrOwnershipMismatch
	}
	if err := b.verifyRef(ref); err != nil {
		return VMRef{}, err
	}
	return ref, nil
}

// safeIncusError preserves only reviewed classifications, never provider text.
func safeIncusError(message string, err error) error {
	for _, kind := range []error{context.Canceled, context.DeadlineExceeded, ErrIncusInstanceMissing, ErrOwnershipMismatch} {
		if errors.Is(err, kind) {
			return errors.Join(errors.New(message), kind)
		}
	}
	return errors.New(message)
}
